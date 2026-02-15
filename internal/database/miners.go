package database

import (
	"database/sql"
	"time"
)

// MinerSessionEntry represents a miner session record.
type MinerSessionEntry struct {
	SessionID      string  `json:"sessionId"`
	Worker         string  `json:"worker"`
	IPAddress      string  `json:"ipAddress"`
	ConnectedAt    int64   `json:"connectedAt"`
	DisconnectedAt int64   `json:"disconnectedAt"`
	SharesAccepted int64   `json:"sharesAccepted"`
	SharesRejected int64   `json:"sharesRejected"`
	BestDifficulty float64 `json:"bestDifficulty"`
}

// UpsertMinerSession inserts or updates a miner session.
func (db *DB) UpsertMinerSession(s MinerSessionEntry) error {
	_, err := db.conn.Exec(`INSERT INTO miner_sessions
		(session_id, worker, ip_address, connected_at, disconnected_at, shares_accepted, shares_rejected, best_difficulty)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			disconnected_at = excluded.disconnected_at,
			shares_accepted = excluded.shares_accepted,
			shares_rejected = excluded.shares_rejected,
			best_difficulty = excluded.best_difficulty`,
		s.SessionID, s.Worker, s.IPAddress, s.ConnectedAt, s.DisconnectedAt,
		s.SharesAccepted, s.SharesRejected, s.BestDifficulty)
	return err
}

// DisconnectMiner marks a session as disconnected.
func (db *DB) DisconnectMiner(sessionID string, disconnectedAt int64) error {
	_, err := db.conn.Exec(`UPDATE miner_sessions SET disconnected_at = ? WHERE session_id = ?`,
		disconnectedAt, sessionID)
	return err
}

// RecentSessions returns the most recent N miner sessions.
func (db *DB) RecentSessions(limit int) ([]MinerSessionEntry, error) {
	rows, err := db.conn.Query(`SELECT session_id, worker, ip_address, connected_at, disconnected_at,
		shares_accepted, shares_rejected, best_difficulty
		FROM miner_sessions ORDER BY connected_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []MinerSessionEntry
	for rows.Next() {
		var s MinerSessionEntry
		if err := rows.Scan(&s.SessionID, &s.Worker, &s.IPAddress, &s.ConnectedAt,
			&s.DisconnectedAt, &s.SharesAccepted, &s.SharesRejected, &s.BestDifficulty); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

// GetWorkerDiff returns the last known difficulty for a worker.
// Returns 0 if the worker has no stored difficulty.
func (db *DB) GetWorkerDiff(worker string) (float64, error) {
	var diff float64
	err := db.conn.QueryRow(`SELECT difficulty FROM worker_diffs WHERE worker = ?`, worker).Scan(&diff)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return diff, err
}

// RecentMinerIPs returns distinct IP addresses of miners that connected in the last 24 hours.
func (db *DB) RecentMinerIPs() ([]string, error) {
	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	rows, err := db.conn.Query(
		`SELECT DISTINCT ip_address FROM miner_sessions WHERE connected_at > ? ORDER BY connected_at DESC`,
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ips []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		ips = append(ips, ip)
	}
	return ips, rows.Err()
}

// SaveWorkerDiff persists the current difficulty for a worker.
func (db *DB) SaveWorkerDiff(worker string, diff float64) error {
	_, err := db.conn.Exec(`INSERT INTO worker_diffs (worker, difficulty, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(worker) DO UPDATE SET difficulty = excluded.difficulty, updated_at = excluded.updated_at`,
		worker, diff, time.Now().Unix())
	return err
}
