package config

func Defaults() *Config {
	return &Config{
		Node: NodeConfig{
			Host:     "127.0.0.1",
			Port:     8332,
			Username: "bitcoin",
			Password: "",
			UseSSL:   false,
		},
		Stratum: StratumConfig{
			Port:           10333,
			MaxConn:        100,
			MaxConnPerIP:   50,
			AuthTimeoutSec: 30,
			AutoStart:      false,
			// Security defaults: match Miningcore's recommended settings.
			BanDurationMinutes:    10,
			InvalidShareThreshold: 0.50, // ban after >50% rejected in window
			InvalidShareWindow:    10,   // evaluate over last 10 shares
			// MaxMsgPerSec/MaxMsgBurst sized for a single high-hashrate ASIC (a
			// 200+ TH/s device at a diff of ~1000 submits ~50 shares/sec), not just
			// the small devices a default install is first tested with. Combined
			// with the proxy-mode per-session vardiff (see proxyRetargetLocked in
			// internal/stratum/session.go), which actively raises a fast miner's
			// diff to curb its own message rate, this is a safety margin for the
			// first few seconds after a big device connects, before that retarget
			// has had a chance to react — not the primary defense.
			MaxMsgPerSec:          50,
			MaxMsgBurst:           150,
			MaxLineSizeKB:         8,    // 8 KB max JSON line
			NTimeMaxDriftSec:      7200, // ±2 hours
		},
		Mining: MiningConfig{
			Coin:          "btc",
			PayoutAddress: "",
			CoinbaseTag:   "/TheVault/",
		},
		Vardiff: VardiffConfig{
			MinDiff:         0.001,
			StartDiff:       1000,
			MaxDiff:         0,
			TargetTimeSec:   15,
			RetargetTimeSec: 90,
			VariancePct:     30,
		},
		App: AppConfig{
			Theme:           "dark",
			LogLevel:        "info",
			ElectricityCost: 0.10,
			DBMaxSizeMB:     250, // cap DB at 250 MB; oldest shares pruned + vacuumed beyond this
		},
		Proxy: ProxyConfig{
			// asicpool.space (Canadian, 0% fee, solo) as the default upstream so a
			// fresh install already has a working pool — WorkerName (the user's own
			// payout address) is left blank; it's wallet-specific and can't default.
			URL:      "asicpool.space:4333",
			Password: "x",
		},
		// Proxy is the default for fresh installs — most home users point GoVault
		// at a pool rather than running their own full node. Existing configs keep
		// whatever mode they saved; the empty-string fallbacks below stay "solo"
		// only for legacy pre-miningMode config files.
		MiningMode: "proxy",
	}
}
