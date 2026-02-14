package node

import (
	"crypto/sha256"
	"encoding/hex"
)

type BlockTemplate struct {
	Version                  int64                 `json:"version"`
	PreviousBlockHash        string                `json:"previousblockhash"`
	Transactions             []TemplateTransaction  `json:"transactions"`
	CoinbaseAux              map[string]string     `json:"coinbaseaux"`
	CoinbaseValue            int64                 `json:"coinbasevalue"`
	Target                   string                `json:"target"`
	MinTime                  int64                 `json:"mintime"`
	Mutable                  []string              `json:"mutable"`
	NonceRange               string                `json:"noncerange"`
	SigOpLimit               int                   `json:"sigoplimit"`
	SizeLimit                int                   `json:"sizelimit"`
	WeightLimit              int                   `json:"weightlimit"`
	CurTime                  int64                 `json:"curtime"`
	Bits                     string                `json:"bits"`
	Height                   int64                 `json:"height"`
	DefaultWitnessCommitment string                `json:"default_witness_commitment"`
	Rules                    []string              `json:"rules"`
	LongPollID               string                `json:"longpollid"`

	// XEC-specific: mandatory coinbase outputs (eCash miner fund + staking rewards)
	CoinbaseTxn *CoinbaseTxnInfo `json:"coinbasetxn,omitempty"`
}

// CoinbaseTxnInfo holds XEC-specific mandatory coinbase output info from getblocktemplate.
type CoinbaseTxnInfo struct {
	MinerFund      *MandatoryOutput `json:"minerfund,omitempty"`
	StakingRewards *MandatoryOutput `json:"stakingrewards,omitempty"`
}

// MandatoryOutput represents a required coinbase output (used by eCash/XEC).
type MandatoryOutput struct {
	Addresses    []string      `json:"addresses,omitempty"`
	MinimumValue int64         `json:"minimumvalue"`
	PayoutScript *PayoutScript `json:"payoutscript,omitempty"`
}

// PayoutScript is a raw hex script for mandatory outputs.
type PayoutScript struct {
	Hex string `json:"hex"`
}

type TemplateTransaction struct {
	Data    string `json:"data"`
	TxID    string `json:"txid"`
	Hash    string `json:"hash"`
	Fee     int64  `json:"fee"`
	SigOps  int    `json:"sigops"`
	Weight  int    `json:"weight"`
}

type BlockchainInfo struct {
	Chain                string  `json:"chain"`
	Blocks               int64   `json:"blocks"`
	Headers              int64   `json:"headers"`
	BestBlockHash        string  `json:"bestblockhash"`
	Difficulty           float64 `json:"difficulty"`
	VerificationProgress float64 `json:"verificationprogress"`
	Pruned               bool    `json:"pruned"`
	InitialBlockDownload bool    `json:"initialblockdownload"`
}

type MiningInfo struct {
	Blocks           int64   `json:"blocks"`
	Difficulty       float64 `json:"difficulty"`
	NetworkHashPS    float64 `json:"networkhashps"`
	PooledTx         int     `json:"pooledtx"`
	Chain            string  `json:"chain"`
}

type NetworkInfo struct {
	Version         int    `json:"version"`
	SubVersion      string `json:"subversion"`
	ProtocolVersion int    `json:"protocolversion"`
	Connections     int    `json:"connections"`
}

type AddressInfo struct {
	IsValid  bool   `json:"isvalid"`
	Address  string `json:"address"`
	IsScript bool   `json:"isscript"`
	IsWitness bool  `json:"iswitness"`
}

// DoubleSHA256 computes SHA256(SHA256(data)).
func DoubleSHA256(data []byte) []byte {
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	return second[:]
}

// ComputeMerkleBranches computes the merkle branch hashes needed for Stratum.
// These are the sibling hashes along the path from the coinbase (leaf 0) to the root.
func ComputeMerkleBranches(txIDs []string) []string {
	if len(txIDs) == 0 {
		return []string{}
	}

	hashes := make([][]byte, len(txIDs))
	for i, txid := range txIDs {
		h, _ := hex.DecodeString(txid)
		// txids from getblocktemplate are already in internal byte order
		hashes[i] = h
	}

	branches := []string{}

	for len(hashes) > 0 {
		// The first hash in the working set is the branch we need
		branches = append(branches, hex.EncodeToString(hashes[0]))

		if len(hashes) == 1 {
			break
		}

		// Pairwise hash the remaining elements
		var next [][]byte
		for i := 1; i < len(hashes); i += 2 {
			var pair []byte
			pair = append(pair, hashes[i]...)
			if i+1 < len(hashes) {
				pair = append(pair, hashes[i+1]...)
			} else {
				pair = append(pair, hashes[i]...)
			}
			next = append(next, DoubleSHA256(pair))
		}
		hashes = next
	}

	return branches
}

// ComputeMerkleBranchesFromTemplate extracts transaction hashes from a block template
// and computes the merkle branches needed for the Stratum mining.notify message.
// The coinbase transaction is NOT included - it will be combined with these branches
// by the miner to compute the merkle root.
func ComputeMerkleBranchesFromTemplate(tmpl *BlockTemplate) []string {
	if len(tmpl.Transactions) == 0 {
		return []string{}
	}

	// For Stratum, merkle branches are computed differently than a full merkle tree.
	// We need the siblings along the path from coinbase to root.
	// With only template transactions (no coinbase), the algorithm is:
	// 1. Start with all tx hashes
	// 2. At each level, take the first hash as a branch, then pairwise-hash the rest
	// But actually for Stratum, it's simpler: we just need the hashes that get paired
	// with the coinbase hash as we walk up the tree.

	hashes := make([][]byte, len(tmpl.Transactions))
	for i, tx := range tmpl.Transactions {
		h, _ := hex.DecodeString(tx.TxID)
		hashes[i] = h
	}

	// The merkle branches for Stratum: at each tree level, the hash that gets
	// paired with the running hash (starting from coinbase).
	// Level 0: hashes[0] (paired with coinbase hash)
	// Level 1: hash of pairs starting from hashes[1]
	// etc.
	branches := []string{}
	current := hashes

	for len(current) > 0 {
		// Take the first element as a branch
		branches = append(branches, hex.EncodeToString(current[0]))

		if len(current) == 1 {
			break
		}

		// Reduce remaining elements by pairwise hashing
		remaining := current[1:]
		var next [][]byte
		for i := 0; i < len(remaining); i += 2 {
			var pair []byte
			pair = append(pair, remaining[i]...)
			if i+1 < len(remaining) {
				pair = append(pair, remaining[i+1]...)
			} else {
				pair = append(pair, remaining[i]...) // duplicate last
			}
			next = append(next, DoubleSHA256(pair))
		}
		current = next
	}

	return branches
}

// MerkleBranchesForStratum computes the standard merkle branches that Stratum expects.
// Given a list of transaction hashes (NOT including coinbase), return the branch
// hashes needed to compute the merkle root when combined with the coinbase hash.
func MerkleBranchesForStratum(txHashes [][]byte) [][]byte {
	if len(txHashes) == 0 {
		return nil
	}

	// Build levels bottom-up, extracting the sibling at each level
	branches := [][]byte{}
	level := txHashes

	for len(level) > 0 {
		// The first element is the sibling for the coinbase path
		branches = append(branches, level[0])

		if len(level) == 1 {
			break
		}

		// Compute next level from remaining elements
		remaining := level[1:]
		var nextLevel [][]byte
		for i := 0; i < len(remaining); i += 2 {
			left := remaining[i]
			var right []byte
			if i+1 < len(remaining) {
				right = remaining[i+1]
			} else {
				right = left
			}
			combined := append(left, right...)
			nextLevel = append(nextLevel, DoubleSHA256(combined))
		}
		level = nextLevel
	}

	return branches
}

// ComputeMerkleRoot computes the merkle root given a coinbase hash and branch hashes.
// This is used during share validation.
func ComputeMerkleRoot(coinbaseHash []byte, branches []string) []byte {
	current := make([]byte, len(coinbaseHash))
	copy(current, coinbaseHash)

	for _, branchHex := range branches {
		branch, _ := hex.DecodeString(branchHex)
		combined := append(current, branch...)
		current = DoubleSHA256(combined)
	}

	return current
}

// ReverseBytes reverses a byte slice in place.
func ReverseBytes(b []byte) {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
}

// StratumPrevHash converts a block hash from getblocktemplate (display order)
// to Stratum's 8-group-of-4-bytes format.
// Step 1: Reverse all bytes from display order to internal byte order.
// Step 2: Swap each 4-byte group (Stratum's prevhash encoding).
func StratumPrevHash(hashHex string) string {
	hashBytes, _ := hex.DecodeString(hashHex)
	// hashHex from getblocktemplate is in display order (big-endian).
	// First reverse all bytes to get internal byte order.
	ReverseBytes(hashBytes)
	// Then swap each 4-byte group for Stratum's prevhash format.
	result := make([]byte, 32)
	for i := 0; i < 8; i++ {
		offset := i * 4
		result[offset+0] = hashBytes[offset+3]
		result[offset+1] = hashBytes[offset+2]
		result[offset+2] = hashBytes[offset+1]
		result[offset+3] = hashBytes[offset+0]
	}
	return hex.EncodeToString(result)
}
