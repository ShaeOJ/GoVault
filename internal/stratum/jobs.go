package stratum

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"govault/internal/coin"
	"govault/internal/node"
	"sync"
	"sync/atomic"
)

// Job represents a mining job sent to miners via mining.notify.
type Job struct {
	ID             string
	PrevHash       string   // Stratum-formatted (4-byte group swapped)
	Coinbase1      string   // hex: before extranonce insertion point
	Coinbase2      string   // hex: after extranonce insertion point
	MerkleBranches []string // hex hashes
	Version        string   // 4 bytes little-endian hex
	NBits          string   // compact target
	NTime          string   // 4 bytes hex

	// Internal data for block reconstruction
	Template *node.BlockTemplate
	SegWit   bool // whether this coin uses SegWit (for block serialization)
}

// JobManager creates and tracks mining jobs from block templates.
type JobManager struct {
	jobs    map[string]*Job
	mu      sync.RWMutex
	nextID  atomic.Uint64
	maxJobs int

	payoutAddress   string
	coinbaseTag     string
	extranonce2Size int
	coinDef         *coin.CoinDef
}

func NewJobManager(payoutAddress, coinbaseTag string, extranonce2Size int, coinDef *coin.CoinDef) *JobManager {
	return &JobManager{
		jobs:            make(map[string]*Job),
		maxJobs:         10,
		payoutAddress:   payoutAddress,
		coinbaseTag:     coinbaseTag,
		extranonce2Size: extranonce2Size,
		coinDef:         coinDef,
	}
}

func (jm *JobManager) SetPayoutAddress(addr string) {
	jm.mu.Lock()
	jm.payoutAddress = addr
	jm.mu.Unlock()
}

// CreateJob builds a new mining job from a block template.
func (jm *JobManager) CreateJob(tmpl *node.BlockTemplate, extranonce1Size int) (*Job, error) {
	if jm.payoutAddress == "" {
		return nil, fmt.Errorf("payout address not configured")
	}

	jobID := fmt.Sprintf("%x", jm.nextID.Add(1))

	// Build coinbase transaction
	coinbase1, coinbase2, err := jm.buildCoinbase(tmpl, extranonce1Size)
	if err != nil {
		return nil, fmt.Errorf("build coinbase: %w", err)
	}

	// Compute merkle branches from template transactions
	branches := []string{} // initialize as empty (not nil) so JSON serializes as []
	if len(tmpl.Transactions) > 0 {
		txHashes := make([][]byte, len(tmpl.Transactions))
		for i, tx := range tmpl.Transactions {
			h, _ := hex.DecodeString(tx.TxID)
			// TxIDs from getblocktemplate are in display order (reversed);
			// reverse to internal byte order for merkle tree computation
			node.ReverseBytes(h)
			txHashes[i] = h
		}
		branchBytes := node.MerkleBranchesForStratum(txHashes)
		for _, b := range branchBytes {
			branches = append(branches, hex.EncodeToString(b))
		}
	}

	// Format version as big-endian hex (Stratum convention: all uint32 fields
	// are sent as BE hex; miners parse as integer and store as LE in the header)
	versionBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(versionBytes, uint32(tmpl.Version))
	version := hex.EncodeToString(versionBytes)

	// Format ntime as hex (big-endian as per Stratum convention)
	ntimeBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(ntimeBytes, uint32(tmpl.CurTime))
	ntime := hex.EncodeToString(ntimeBytes)

	job := &Job{
		ID:             jobID,
		PrevHash:       node.StratumPrevHash(tmpl.PreviousBlockHash),
		Coinbase1:      coinbase1,
		Coinbase2:      coinbase2,
		MerkleBranches: branches,
		Version:        version,
		NBits:          tmpl.Bits,
		NTime:          ntime,
		Template:       tmpl,
		SegWit:         jm.coinDef.SegWit,
	}

	jm.mu.Lock()
	jm.jobs[jobID] = job
	// Trim old jobs
	if len(jm.jobs) > jm.maxJobs {
		var oldest string
		var oldestID uint64 = ^uint64(0)
		for id := range jm.jobs {
			var idNum uint64
			fmt.Sscanf(id, "%x", &idNum)
			if idNum < oldestID {
				oldestID = idNum
				oldest = id
			}
		}
		delete(jm.jobs, oldest)
	}
	jm.mu.Unlock()

	return job, nil
}

// RegisterUpstreamJob creates a Job from raw upstream stratum fields.
// Unlike CreateJob, this does not build a coinbase or merkle branches — it
// stores the upstream-provided values directly. Template is nil.
func (jm *JobManager) RegisterUpstreamJob(
	jobID, prevHash, coinbase1, coinbase2 string,
	merkleBranches []string,
	version, nbits, ntime string,
	cleanJobs bool,
) *Job {
	if merkleBranches == nil {
		merkleBranches = []string{}
	}

	job := &Job{
		ID:             jobID,
		PrevHash:       prevHash,
		Coinbase1:      coinbase1,
		Coinbase2:      coinbase2,
		MerkleBranches: merkleBranches,
		Version:        version,
		NBits:          nbits,
		NTime:          ntime,
		Template:       nil, // proxy mode: no local template
	}

	jm.mu.Lock()
	if cleanJobs {
		jm.jobs = make(map[string]*Job)
	}
	jm.jobs[jobID] = job
	// Trim old jobs
	if len(jm.jobs) > jm.maxJobs {
		var oldest string
		var oldestAge int
		for id := range jm.jobs {
			// In proxy mode job IDs are opaque strings from upstream,
			// so we just count backwards to find the oldest entry.
			oldestAge++
			if oldestAge == 1 || id < oldest {
				oldest = id
			}
		}
		if oldest != jobID {
			delete(jm.jobs, oldest)
		}
	}
	jm.mu.Unlock()

	return job
}

func (jm *JobManager) GetJob(id string) *Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	return jm.jobs[id]
}

func (jm *JobManager) ActiveJobIDs() map[string]bool {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	ids := make(map[string]bool, len(jm.jobs))
	for id := range jm.jobs {
		ids[id] = true
	}
	return ids
}

func (jm *JobManager) CleanJobs() {
	jm.mu.Lock()
	jm.jobs = make(map[string]*Job)
	jm.mu.Unlock()
}

// buildCoinbase constructs the coinbase transaction and splits it into
// coinbase1 (before extranonce) and coinbase2 (after extranonce).
// For Stratum, coinbase1+extranonce1+extranonce2+coinbase2 must be the "stripped"
// transaction (no SegWit marker/flag/witness) so miners compute the correct TXID
// for the merkle root. SegWit data is added back in buildFullBlock for block submission.
func (jm *JobManager) buildCoinbase(tmpl *node.BlockTemplate, extranonce1Size int) (string, string, error) {
	var tx []byte

	// Version (4 bytes, little-endian) - use version 2 for BIP68
	tx = append(tx, 0x02, 0x00, 0x00, 0x00)

	// NOTE: SegWit marker (0x00, 0x01) is intentionally NOT included here.
	// The coinbase sent to miners must be the stripped TX so that hashing it
	// produces the TXID (not WTXID) for the merkle tree. The marker and
	// witness data are added in buildFullBlock when submitting a found block.

	// Input count (1 - the coinbase input)
	tx = append(tx, 0x01)

	// Previous outpoint (32 zero bytes + index 0xFFFFFFFF)
	tx = append(tx, make([]byte, 32)...)
	tx = append(tx, 0xff, 0xff, 0xff, 0xff)

	// ScriptSig
	scriptSig := jm.buildScriptSig(tmpl.Height, extranonce1Size)
	tx = append(tx, byte(len(scriptSig)+extranonce1Size+jm.extranonce2Size))
	tx = append(tx, scriptSig...)

	// Mark the split point - everything so far is coinbase1
	coinbase1 := hex.EncodeToString(tx)

	// After the extranonce space, continue with rest of transaction
	var tx2 []byte

	// Sequence (0xFFFFFFFF)
	tx2 = append(tx2, 0xff, 0xff, 0xff, 0xff)

	// === Outputs ===

	// Calculate output count
	outputCount := 1 // payout output

	hasWitnessCommitment := jm.coinDef.SegWit && tmpl.DefaultWitnessCommitment != ""
	if hasWitnessCommitment {
		outputCount++ // witness commitment output
	}

	// XEC mandatory outputs
	hasMinerFund := jm.coinDef.HasMinerFund && tmpl.CoinbaseTxn != nil && tmpl.CoinbaseTxn.MinerFund != nil
	hasStakingReward := jm.coinDef.HasStakingReward && tmpl.CoinbaseTxn != nil && tmpl.CoinbaseTxn.StakingRewards != nil
	if hasMinerFund {
		outputCount++
	}
	if hasStakingReward {
		outputCount++
	}

	tx2 = appendCompactSize(tx2, uint64(outputCount))

	// Calculate payout value (subtract mandatory outputs for XEC)
	payoutValue := tmpl.CoinbaseValue
	var minerFundValue int64
	var stakingRewardValue int64
	if hasMinerFund {
		minerFundValue = tmpl.CoinbaseTxn.MinerFund.MinimumValue
		payoutValue -= minerFundValue
	}
	if hasStakingReward {
		stakingRewardValue = tmpl.CoinbaseTxn.StakingRewards.MinimumValue
		payoutValue -= stakingRewardValue
	}

	// Output 0: Payout to configured address
	valueBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(valueBytes, uint64(payoutValue))
	tx2 = append(tx2, valueBytes...)

	// ScriptPubKey for payout address
	scriptPubKey, err := coin.AddressToScriptPubKey(jm.coinDef, jm.payoutAddress)
	if err != nil {
		return "", "", fmt.Errorf("address to script: %w", err)
	}
	tx2 = appendVarBytes(tx2, scriptPubKey)

	// Output (SegWit only): Witness commitment
	if hasWitnessCommitment {
		// Value: 0
		tx2 = append(tx2, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
		// Script: OP_RETURN + witness commitment
		commitmentScript, _ := hex.DecodeString(tmpl.DefaultWitnessCommitment)
		tx2 = appendVarBytes(tx2, commitmentScript)
	}

	// XEC mandatory output: Miner fund
	if hasMinerFund {
		fundValueBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(fundValueBytes, uint64(minerFundValue))
		tx2 = append(tx2, fundValueBytes...)

		fundScript, err := jm.getMandatoryOutputScript(tmpl.CoinbaseTxn.MinerFund)
		if err != nil {
			return "", "", fmt.Errorf("miner fund script: %w", err)
		}
		tx2 = appendVarBytes(tx2, fundScript)
	}

	// XEC mandatory output: Staking reward
	if hasStakingReward {
		stakeValueBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(stakeValueBytes, uint64(stakingRewardValue))
		tx2 = append(tx2, stakeValueBytes...)

		stakeScript, err := jm.getMandatoryOutputScript(tmpl.CoinbaseTxn.StakingRewards)
		if err != nil {
			return "", "", fmt.Errorf("staking reward script: %w", err)
		}
		tx2 = appendVarBytes(tx2, stakeScript)
	}

	// NOTE: SegWit witness data is intentionally NOT included here.
	// It is added in buildFullBlock when submitting a found block.

	// Locktime (0x00000000)
	tx2 = append(tx2, 0x00, 0x00, 0x00, 0x00)

	coinbase2 := hex.EncodeToString(tx2)

	return coinbase1, coinbase2, nil
}

// getMandatoryOutputScript gets the scriptPubKey for an XEC mandatory output.
// It tries the raw PayoutScript first, then falls back to decoding the address.
func (jm *JobManager) getMandatoryOutputScript(output *node.MandatoryOutput) ([]byte, error) {
	// Prefer raw payout script hex if available
	if output.PayoutScript != nil && output.PayoutScript.Hex != "" {
		return hex.DecodeString(output.PayoutScript.Hex)
	}

	// Fall back to decoding the first address
	if len(output.Addresses) > 0 {
		return coin.AddressToScriptPubKey(jm.coinDef, output.Addresses[0])
	}

	return nil, fmt.Errorf("no script or address available for mandatory output")
}

// buildScriptSig builds the coinbase scriptSig up to the extranonce insertion point.
func (jm *JobManager) buildScriptSig(height int64, extranonce1Size int) []byte {
	var script []byte

	// BIP34: block height as CScriptNum
	heightBytes := encodeHeight(height)
	script = append(script, heightBytes...)

	// Coinbase tag
	if jm.coinbaseTag != "" {
		tag := []byte(jm.coinbaseTag)
		if len(tag) > 80 {
			tag = tag[:80]
		}
		script = append(script, tag...)
	}

	return script
}

// encodeHeight encodes a block height for the coinbase scriptSig (BIP34).
func encodeHeight(height int64) []byte {
	if height <= 16 {
		return []byte{byte(0x50 + height)} // OP_1 through OP_16
	}

	// Encode as minimal CScriptNum
	var heightBytes []byte
	h := height
	for h > 0 {
		heightBytes = append(heightBytes, byte(h&0xff))
		h >>= 8
	}
	// If the high bit is set, add a zero byte
	if heightBytes[len(heightBytes)-1]&0x80 != 0 {
		heightBytes = append(heightBytes, 0x00)
	}

	result := []byte{byte(len(heightBytes))}
	result = append(result, heightBytes...)
	return result
}

// appendVarBytes appends a variable-length byte slice with its compact size prefix.
func appendVarBytes(buf []byte, data []byte) []byte {
	buf = appendCompactSize(buf, uint64(len(data)))
	return append(buf, data...)
}

// appendCompactSize appends a Bitcoin compact size encoding.
func appendCompactSize(buf []byte, n uint64) []byte {
	switch {
	case n < 0xfd:
		return append(buf, byte(n))
	case n <= 0xffff:
		buf = append(buf, 0xfd)
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(n))
		return append(buf, b...)
	case n <= 0xffffffff:
		buf = append(buf, 0xfe)
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(n))
		return append(buf, b...)
	default:
		buf = append(buf, 0xff)
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, n)
		return append(buf, b...)
	}
}
