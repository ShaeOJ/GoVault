package database

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
