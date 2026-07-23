package main

import "time"

// NodeState is the last-polled snapshot for one edge node.
type NodeState struct {
	Entry    NodeEntry
	Online    bool
	PingMs    int64   // -1 = unreachable
	AvgPingMs int64   // rolling average of last 10 successful pings
	pingHistory []int64 // internal — not serialised
	failStreak  int     // internal — consecutive poll failures
	LastSeen  time.Time
	Error     string

	NodeID    string
	NodeName  string
	UptimeSec int64

	Hashrate     float64
	ActiveMiners int
	SharesFwd    int64
	SharesAcc    int64
	SharesRej    int64
	BestDiff     float64
	BlocksFound  int64
	NetworkDiff  float64

	MaxConn        int
	MaxConnPerIP   int
	AuthTimeoutSec int
	LogLevel       string
	TokenOK        bool // false if settings fetch returned 401/404

	Pairs  []PairSnapshot
	Miners []MinerSnapshot

	PublicIP string
	LocalIPs []string
	Lat      float64
	Lng      float64
}

type PairSnapshot struct {
	Coin         string  `json:"coin"`
	Mode         string  `json:"mode"`
	LocalPort    int     `json:"localPort"`
	Connected    bool    `json:"connected"`
	Authorized   bool    `json:"authorized"`
	UpstreamDiff float64 `json:"upstreamDiff"`
	NetworkDiff  float64 `json:"networkDiff"`
	MinerCount   int     `json:"minerCount"`
	BlockHeight  int64   `json:"blockHeight"`
}

type MinerSnapshot struct {
	WorkerName  string  `json:"workerName"`
	Hashrate    float64 `json:"hashrate"`
	Accepted    int64   `json:"sharesAccepted"`
	Rejected    int64   `json:"sharesRejected"`
	IPAddress   string  `json:"ipAddress"`
	UserAgent   string  `json:"userAgent"`
	CurrentDiff float64 `json:"currentDiff"`
}
