package database

import (
	"fmt"
	"strings"
	"time"
)

// InsertShares batch-inserts share entries.
func (db *DB) InsertShares(shares []ShareEntry) error {
	if len(shares) == 0 {
		return nil
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO shares (timestamp, miner_id, worker, difficulty, accepted, reject_reason)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, s := range shares {
		accepted := 0
		if s.Accepted {
			accepted = 1
		}
		if _, err := stmt.Exec(s.Timestamp, s.MinerID, s.Worker, s.Difficulty, accepted, s.RejectReason); err != nil {
			tx.Rollback()
			return fmt.Errorf("exec: %w", err)
		}
	}

	return tx.Commit()
}

// ShareCount returns the total count of accepted and rejected shares.
func (db *DB) ShareCount() (accepted, rejected uint64, err error) {
	row := db.conn.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN accepted=1 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN accepted=0 THEN 1 ELSE 0 END), 0)
		FROM shares`)
	err = row.Scan(&accepted, &rejected)
	return
}

// RecentShares returns the most recent N shares.
func (db *DB) RecentShares(limit int) ([]ShareEntry, error) {
	rows, err := db.conn.Query(`SELECT timestamp, miner_id, worker, difficulty, accepted, reject_reason
		FROM shares ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ShareEntry
	for rows.Next() {
		var s ShareEntry
		var acc int
		if err := rows.Scan(&s.Timestamp, &s.MinerID, &s.Worker, &s.Difficulty, &acc, &s.RejectReason); err != nil {
			return nil, err
		}
		s.Accepted = acc == 1
		result = append(result, s)
	}
	return result, rows.Err()
}

// PruneShares deletes shares older than the given duration.
func (db *DB) PruneShares(maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge).Unix()
	result, err := db.conn.Exec(`DELETE FROM shares WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// ClearRejectedShares deletes all rejected share rows and zeros the cumulative rejected counter.
func (db *DB) ClearRejectedShares() (int64, error) {
	result, err := db.conn.Exec(`DELETE FROM shares WHERE accepted = 0`)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()

	_, err = db.conn.Exec(`UPDATE cumulative_stats SET total_rejected = 0 WHERE id = 1`)
	if err != nil {
		return n, err
	}
	return n, nil
}

// BestShareDifficulty returns the highest difficulty share ever recorded.
func (db *DB) BestShareDifficulty() (float64, error) {
	var best float64
	err := db.conn.QueryRow(`SELECT COALESCE(MAX(difficulty), 0) FROM shares WHERE accepted=1`).Scan(&best)
	return best, err
}

// InsertShare inserts a single share (used for non-buffered writes).
func (db *DB) InsertShare(s ShareEntry) error {
	return db.InsertShares([]ShareEntry{s})
}

// ShareCountByMiner returns per-miner share counts.
func (db *DB) ShareCountByMiner() (map[string][2]uint64, error) {
	rows, err := db.conn.Query(`SELECT miner_id,
		SUM(CASE WHEN accepted=1 THEN 1 ELSE 0 END),
		SUM(CASE WHEN accepted=0 THEN 1 ELSE 0 END)
		FROM shares GROUP BY miner_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][2]uint64)
	for rows.Next() {
		var id string
		var acc, rej uint64
		if err := rows.Scan(&id, &acc, &rej); err != nil {
			return nil, err
		}
		result[id] = [2]uint64{acc, rej}
	}
	return result, rows.Err()
}

// buildPlaceholders builds "(?,?,?),(?,?,?),..." for batch inserts.
func buildPlaceholders(rowCount, colCount int) string {
	row := "(" + strings.Repeat("?,", colCount-1) + "?)"
	rows := make([]string, rowCount)
	for i := range rows {
		rows[i] = row
	}
	return strings.Join(rows, ",")
}
