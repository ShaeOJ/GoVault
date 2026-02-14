package database

// CumulativeStats holds the single-row summary of all-time stats.
type CumulativeStats struct {
	TotalAccepted  uint64  `json:"totalAccepted"`
	TotalRejected  uint64  `json:"totalRejected"`
	BestDifficulty float64 `json:"bestDifficulty"`
	BlocksFound    uint64  `json:"blocksFound"`
}

// LoadCumulativeStats reads the cumulative stats row.
func (db *DB) LoadCumulativeStats() (CumulativeStats, error) {
	var s CumulativeStats
	err := db.conn.QueryRow(`SELECT total_accepted, total_rejected, best_difficulty, blocks_found
		FROM cumulative_stats WHERE id = 1`).
		Scan(&s.TotalAccepted, &s.TotalRejected, &s.BestDifficulty, &s.BlocksFound)
	return s, err
}

// SaveCumulativeStats writes the cumulative stats row.
func (db *DB) SaveCumulativeStats(s CumulativeStats) error {
	_, err := db.conn.Exec(`UPDATE cumulative_stats SET
		total_accepted = ?, total_rejected = ?, best_difficulty = ?, blocks_found = ?
		WHERE id = 1`,
		s.TotalAccepted, s.TotalRejected, s.BestDifficulty, s.BlocksFound)
	return err
}
