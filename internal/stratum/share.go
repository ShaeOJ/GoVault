package stratum

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"govault/internal/node"
	"math/big"
	"sync"
)

// pdiff1Target is the target for difficulty 1 in pool difficulty.
// 0x00000000FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF
var pdiff1Target *big.Int

func init() {
	pdiff1Target = new(big.Int)
	pdiff1Target.SetString("00000000FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF", 16)
}

// ShareSubmission holds the data submitted by a miner.
type ShareSubmission struct {
	WorkerName  string
	JobID       string
	Extranonce2 string
	NTime       string
	Nonce       string
	VersionBits string // optional: version rolling bits from mining.submit param 6
	VersionMask uint32 // negotiated mask from mining.configure
}

// ShareResult is the outcome of validating a share.
type ShareResult struct {
	Valid      bool
	BlockFound bool
	Difficulty float64
	BlockHash  string
	BlockHex   string
}

// ShareValidator validates submitted shares against job data.
type ShareValidator struct {
	jobManager *JobManager
	duplicates map[string]map[string]bool // jobID -> set of "en2+ntime+nonce"
	mu         sync.Mutex
}

func NewShareValidator(jm *JobManager) *ShareValidator {
	return &ShareValidator{
		jobManager: jm,
		duplicates: make(map[string]map[string]bool),
	}
}

// ValidateShare validates a share submission.
// For a solo pool, we don't reject for low difficulty — any valid hash
// that meets the network target wins the block regardless of pool difficulty.
func (sv *ShareValidator) ValidateShare(extranonce1 string, sub ShareSubmission) (*ShareResult, *StratumError) {
	job := sv.jobManager.GetJob(sub.JobID)
	if job == nil {
		return nil, NewError(ErrStaleJob, "job not found")
	}

	// Check for duplicate (include version bits for version-rolling miners)
	dupeKey := sub.Extranonce2 + sub.NTime + sub.Nonce + sub.VersionBits
	sv.mu.Lock()
	if sv.duplicates[sub.JobID] == nil {
		sv.duplicates[sub.JobID] = make(map[string]bool)
	}
	if sv.duplicates[sub.JobID][dupeKey] {
		sv.mu.Unlock()
		return nil, NewError(ErrDuplicate, "duplicate share")
	}
	sv.duplicates[sub.JobID][dupeKey] = true
	sv.mu.Unlock()

	// Reconstruct coinbase transaction
	coinbaseHex := job.Coinbase1 + extranonce1 + sub.Extranonce2 + job.Coinbase2
	coinbaseBytes, err := hex.DecodeString(coinbaseHex)
	if err != nil {
		return nil, NewError(ErrOther, "invalid coinbase hex")
	}

	// Double SHA256 the coinbase to get coinbase hash
	coinbaseHash := node.DoubleSHA256(coinbaseBytes)

	// Compute merkle root
	merkleRoot := node.ComputeMerkleRoot(coinbaseHash, job.MerkleBranches)

	// Construct 80-byte block header
	header, err := buildBlockHeader(job, merkleRoot, sub.NTime, sub.Nonce, sub.VersionBits, sub.VersionMask)
	if err != nil {
		return nil, NewError(ErrOther, fmt.Sprintf("build header: %v", err))
	}

	// Double SHA256 the header
	blockHash := node.DoubleSHA256(header)

	// Convert hash to big.Int (it's in little-endian, reverse for comparison)
	hashReversed := make([]byte, 32)
	copy(hashReversed, blockHash)
	node.ReverseBytes(hashReversed)
	hashInt := new(big.Int).SetBytes(hashReversed)

	// Calculate share difficulty
	shareDiff := new(big.Float).SetInt(pdiff1Target)
	hashFloat := new(big.Float).SetInt(hashInt)
	if hashInt.Sign() == 0 {
		// Hash is zero - extremely unlikely but valid
		shareDiff = new(big.Float).SetFloat64(1e18)
	} else {
		shareDiff.Quo(shareDiff, hashFloat)
	}
	actualDiff, _ := shareDiff.Float64()

	result := &ShareResult{
		Valid:      true,
		Difficulty: actualDiff,
	}

	// Check if this meets the network target (block found!)
	networkTarget := CompactToBig(job.NBits)
	if hashInt.Cmp(networkTarget) <= 0 {
		result.BlockFound = true
		// Hash in display order (reversed)
		result.BlockHash = hex.EncodeToString(hashReversed)
		// Build full block hex for submission (only in solo mode where Template is set)
		if job.Template != nil {
			blockHex, err := buildFullBlock(job, coinbaseBytes, header)
			if err == nil {
				result.BlockHex = blockHex
			}
		}
	}

	return result, nil
}

// CleanDuplicates removes duplicate tracking for old jobs.
func (sv *ShareValidator) CleanDuplicates(keepJobIDs map[string]bool) {
	sv.mu.Lock()
	for id := range sv.duplicates {
		if !keepJobIDs[id] {
			delete(sv.duplicates, id)
		}
	}
	sv.mu.Unlock()
}

// buildBlockHeader constructs the 80-byte block header.
// All uint32 fields (version, nTime, nBits, nonce) are sent/submitted as
// big-endian hex in Stratum and must be reversed to little-endian for the header.
func buildBlockHeader(job *Job, merkleRoot []byte, ntimeHex, nonceHex, versionBitsHex string, versionMask uint32) ([]byte, error) {
	header := make([]byte, 80)

	// Version (4 bytes) - sent as BE hex, convert to LE for header
	versionBytes, _ := hex.DecodeString(job.Version)
	versionInt := binary.BigEndian.Uint32(versionBytes)

	// Apply version rolling bits if present (XOR delta from the base version)
	if versionBitsHex != "" && versionMask != 0 {
		vbBytes, err := hex.DecodeString(versionBitsHex)
		if err == nil && len(vbBytes) == 4 {
			rolledBits := binary.BigEndian.Uint32(vbBytes)
			versionInt = versionInt ^ (rolledBits & versionMask)
		}
	}
	binary.LittleEndian.PutUint32(header[0:4], versionInt)

	// Previous block hash (32 bytes) - convert from Stratum format back to internal order
	prevHashBytes, _ := hex.DecodeString(job.PrevHash)
	// Stratum sends prevhash with 4-byte groups swapped; reverse each group back
	for i := 0; i < 8; i++ {
		offset := i * 4
		header[4+offset+0] = prevHashBytes[offset+3]
		header[4+offset+1] = prevHashBytes[offset+2]
		header[4+offset+2] = prevHashBytes[offset+1]
		header[4+offset+3] = prevHashBytes[offset+0]
	}

	// Merkle root (32 bytes, already in internal byte order)
	copy(header[36:68], merkleRoot)

	// nTime (4 bytes) - sent as BE hex, reverse to LE for the header
	ntimeBytes, err := hex.DecodeString(ntimeHex)
	if err != nil {
		return nil, fmt.Errorf("invalid ntime hex: %w", err)
	}
	if len(ntimeBytes) != 4 {
		return nil, fmt.Errorf("invalid ntime length: got %d bytes, want 4", len(ntimeBytes))
	}
	header[68] = ntimeBytes[3]
	header[69] = ntimeBytes[2]
	header[70] = ntimeBytes[1]
	header[71] = ntimeBytes[0]

	// nBits (4 bytes) - from template as BE hex, reverse to LE for the header
	nbitsBytes, _ := hex.DecodeString(job.NBits)
	header[72] = nbitsBytes[3]
	header[73] = nbitsBytes[2]
	header[74] = nbitsBytes[1]
	header[75] = nbitsBytes[0]

	// Nonce (4 bytes) - submitted as BE hex, reverse to LE for the header
	nonceBytes, err := hex.DecodeString(nonceHex)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce hex: %w", err)
	}
	if len(nonceBytes) != 4 {
		return nil, fmt.Errorf("invalid nonce length: got %d bytes, want 4", len(nonceBytes))
	}
	header[76] = nonceBytes[3]
	header[77] = nonceBytes[2]
	header[78] = nonceBytes[1]
	header[79] = nonceBytes[0]

	return header, nil
}

// buildFullBlock serializes the complete block for submitblock.
// The header from buildBlockHeader is already in correct wire format (all
// uint32 fields in LE). For SegWit coins, the stripped coinbase TX is
// reconstructed with SegWit marker, flag, and witness data.
func buildFullBlock(job *Job, coinbaseTx []byte, header []byte) (string, error) {
	var block []byte

	// Header is already in wire format (all fields LE)
	block = append(block, header...)

	// Transaction count
	txCount := uint64(1 + len(job.Template.Transactions))
	block = appendCompactSize(block, txCount)

	// Coinbase transaction — for SegWit, add back marker/flag/witness
	if job.SegWit {
		// The stripped coinbase is: version(4) + body + locktime(4)
		// Full SegWit coinbase: version(4) + 0x00 + 0x01 + body + witness + locktime(4)
		version := coinbaseTx[0:4]
		body := coinbaseTx[4 : len(coinbaseTx)-4]
		locktime := coinbaseTx[len(coinbaseTx)-4:]

		block = append(block, version...)
		block = append(block, 0x00, 0x01) // SegWit marker + flag
		block = append(block, body...)
		// Coinbase witness: 1 stack item of 32 zero bytes
		block = append(block, 0x01)                    // stack count
		block = append(block, 0x20)                    // 32 bytes
		block = append(block, make([]byte, 32)...)
		block = append(block, locktime...)
	} else {
		block = append(block, coinbaseTx...)
	}

	// Template transactions
	for _, tx := range job.Template.Transactions {
		txBytes, err := hex.DecodeString(tx.Data)
		if err != nil {
			return "", fmt.Errorf("decode tx: %w", err)
		}
		block = append(block, txBytes...)
	}

	return hex.EncodeToString(block), nil
}

// Pdiff1Target returns the pool difficulty-1 target (used for difficulty calculations).
func Pdiff1Target() *big.Int {
	return new(big.Int).Set(pdiff1Target)
}

// CompactToBig converts an nBits compact target to a big.Int.
func CompactToBig(nbitsHex string) *big.Int {
	nbitsBytes, _ := hex.DecodeString(nbitsHex)
	if len(nbitsBytes) != 4 {
		return new(big.Int)
	}

	compact := binary.BigEndian.Uint32(nbitsBytes)

	exponent := compact >> 24
	mantissa := compact & 0x007fffff

	var target big.Int
	if exponent <= 3 {
		mantissa >>= 8 * (3 - exponent)
		target.SetInt64(int64(mantissa))
	} else {
		target.SetInt64(int64(mantissa))
		target.Lsh(&target, 8*(uint(exponent)-3))
	}

	if compact&0x00800000 != 0 {
		target.Neg(&target)
	}

	return &target
}

// DifficultyToTarget converts a pool difficulty to a target big.Int.
func DifficultyToTarget(diff float64) *big.Int {
	if diff <= 0 {
		return new(big.Int).Set(pdiff1Target)
	}

	// target = pdiff1 / diff
	diffFloat := new(big.Float).SetFloat64(diff)
	targetFloat := new(big.Float).SetInt(pdiff1Target)
	targetFloat.Quo(targetFloat, diffFloat)

	target, _ := targetFloat.Int(nil)
	return target
}
