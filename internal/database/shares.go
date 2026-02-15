package database

import (
	"fmt"
	"math"
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

	stmt, err := tx.Prepare(`INSERT INTO shares (timestamp, miner_id, worker, difficulty, accepted, reject_reason, session_diff)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
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
		if _, err := stmt.Exec(s.Timestamp, s.MinerID, s.Worker, s.Difficulty, accepted, s.RejectReason, s.SessionDiff); err != nil {
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

// MinerHashrateHistory computes per-miner hashrate over time buckets from the
// shares table. Only rows with session_diff > 0 are included, so pre-migration
// data is excluded gracefully.
func (db *DB) MinerHashrateHistory(minerID string, since int64, bucketSec int64) ([]HashrateEntry, error) {
	rows, err := db.conn.Query(
		`SELECT timestamp, session_diff FROM shares
		 WHERE miner_id = ? AND timestamp >= ? AND accepted = 1 AND session_diff > 0
		 ORDER BY timestamp`, minerID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type bucket struct {
		sumDiff float64
	}
	buckets := make(map[int64]*bucket)

	for rows.Next() {
		var ts int64
		var sd float64
		if err := rows.Scan(&ts, &sd); err != nil {
			return nil, err
		}
		key := (ts / bucketSec) * bucketSec
		b, ok := buckets[key]
		if !ok {
			b = &bucket{}
			buckets[key] = b
		}
		b.sumDiff += sd
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(buckets) == 0 {
		return nil, nil
	}

	// Collect and sort bucket keys
	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	// Simple insertion sort (small slice)
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}

	result := make([]HashrateEntry, 0, len(keys))
	for _, k := range keys {
		hashrate := buckets[k].sumDiff * math.Pow(2, 32) / float64(bucketSec)
		result = append(result, HashrateEntry{
			Timestamp: k,
			Hashrate:  hashrate,
		})
	}

	return result, nil
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
