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
			MaxMsgPerSec:          20,   // 20 msg/s sustained, 50-burst
			MaxMsgBurst:           50,
			MaxLineSizeKB:         8,    // 8 KB max JSON line
			NTimeMaxDriftSec:      7200, // ±2 hours
		},
		Mining: MiningConfig{
			Coin:          "btc",
			PayoutAddress: "",
			CoinbaseTag:   "/GoVault/",
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
		},
		Proxy: ProxyConfig{
			Password: "x",
		},
		MiningMode: "solo",
	}
}
