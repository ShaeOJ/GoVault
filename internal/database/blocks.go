package database

import "time"

// BlockEntry represents a found block.
type BlockEntry struct {
	Timestamp  int64   `json:"timestamp"`
	Height     int64   `json:"height"`
	Hash       string  `json:"hash"`
	MinerID    string  `json:"minerId"`
	Worker     string  `json:"worker"`
	Difficulty float64 `json:"difficulty"`
}

// InsertBlock records a found block.
func (db *DB) InsertBlock(b BlockEntry) error {
	_, err := db.conn.Exec(`INSERT INTO blocks (timestamp, height, hash, miner_id, worker, difficulty)
		VALUES (?, ?, ?, ?, ?, ?)`,
		b.Timestamp, b.Height, b.Hash, b.MinerID, b.Worker, b.Difficulty)
	return err
}

// BlockCount returns the total number of blocks found.
func (db *DB) BlockCount() (uint64, error) {
	var count uint64
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM blocks`).Scan(&count)
	return count, err
}

// RecentBlocks returns the most recent N blocks.
func (db *DB) RecentBlocks(limit int) ([]BlockEntry, error) {
	rows, err := db.conn.Query(`SELECT timestamp, height, hash, miner_id, worker, difficulty
		FROM blocks ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []BlockEntry
	for rows.Next() {
		var b BlockEntry
		if err := rows.Scan(&b.Timestamp, &b.Height, &b.Hash, &b.MinerID, &b.Worker, &b.Difficulty); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// PruneBlocks deletes blocks older than the given duration.
func (db *DB) PruneBlocks(maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge).Unix()
	result, err := db.conn.Exec(`DELETE FROM blocks WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
