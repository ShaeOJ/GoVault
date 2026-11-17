package database

import "time"

// HashrateEntry represents a hashrate data point.
type HashrateEntry struct {
	Timestamp       int64   `json:"t"`
	Hashrate        float64 `json:"h"`
	SharesPerSecond float64 `json:"s"`
}

// InsertHashrate records a hashrate data point.
func (db *DB) InsertHashrate(timestamp int64, hashrate, sharesPerSecond float64) error {
	_, err := db.conn.Exec(`INSERT INTO hashrate_history (timestamp, hashrate, shares_per_sec) VALUES (?, ?, ?)`,
		timestamp, hashrate, sharesPerSecond)
	return err
}

// LoadHashrateHistory returns hashrate points since the given cutoff timestamp.
func (db *DB) LoadHashrateHistory(since int64) ([]HashrateEntry, error) {
	rows, err := db.conn.Query(`SELECT timestamp, hashrate, shares_per_sec FROM hashrate_history
		WHERE timestamp >= ? ORDER BY timestamp ASC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []HashrateEntry
	for rows.Next() {
		var e HashrateEntry
		if err := rows.Scan(&e.Timestamp, &e.Hashrate, &e.SharesPerSecond); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// PruneHashrate deletes hashrate entries older than the given duration.
func (db *DB) PruneHashrate(maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge).Unix()
	result, err := db.conn.Exec(`DELETE FROM hashrate_history WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
