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
			Port:      10333,
			MaxConn:   100,
			AutoStart: false,
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
			Theme:    "dark",
			LogLevel: "info",
		},
	}
}
