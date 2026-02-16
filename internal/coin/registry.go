package coin

// Coins maps coin ID strings to their full definitions.
var Coins = map[string]*CoinDef{
	"btc": {
		Name:               "Bitcoin",
		Symbol:             "BTC",
		CoinID:             "btc",
		SegWit:             true,
		Bech32HRP:          "bc",
		P2PKHVersion:       0x00,
		P2SHVersion:        0x05,
		P2PKHPrefixChar:    '1',
		P2SHPrefixChar:     '3',
		DefaultRPCPort:     8332,
		DefaultRPCUsername: "bitcoin",
		GBTRules:           []string{"segwit"},
		TargetBlockTimeSec: 600,
	},
	"bch": {
		Name:               "Bitcoin Cash",
		Symbol:             "BCH",
		CoinID:             "bch",
		SegWit:             false,
		CashAddrPrefix:     "bitcoincash",
		P2PKHVersion:       0x00,
		P2SHVersion:        0x05,
		DefaultRPCPort:     8332,
		DefaultRPCUsername: "bitcoincash",
		GBTRules:           []string{},
		TargetBlockTimeSec: 600,
	},
	"dgb": {
		Name:               "DigiByte",
		Symbol:             "DGB",
		CoinID:             "dgb",
		SegWit:             true,
		Bech32HRP:          "dgb",
		P2PKHVersion:       0x1e,
		P2SHVersion:        0x3f,
		P2PKHPrefixChar:    'D',
		P2SHPrefixChar:     'S',
		DefaultRPCPort:     14022,
		DefaultRPCUsername: "digibyte",
		GBTRules:           []string{"segwit"},
		TargetBlockTimeSec: 60,
		MiningAlgo:         "sha256d",
	},
	"bc2": {
		Name:               "Bitcoin II",
		Symbol:             "BC2",
		CoinID:             "bc2",
		SegWit:             true,
		Bech32HRP:          "bc",
		P2PKHVersion:       0x00,
		P2SHVersion:        0x05,
		P2PKHPrefixChar:    '1',
		P2SHPrefixChar:     '3',
		DefaultRPCPort:     8332,
		DefaultRPCUsername: "bitcoin",
		GBTRules:           []string{"segwit"},
		TargetBlockTimeSec: 600,
	},
	"xec": {
		Name:               "eCash",
		Symbol:             "XEC",
		CoinID:             "xec",
		SegWit:             false,
		CashAddrPrefix:     "ecash",
		P2PKHVersion:       0x00,
		P2SHVersion:        0x05,
		DefaultRPCPort:     8332,
		DefaultRPCUsername: "ecash",
		GBTRules:           []string{},
		TargetBlockTimeSec: 600,
		HasMinerFund:       true,
		HasStakingReward:   true,
	},
}

// Get returns the CoinDef for a coin ID, defaulting to BTC if not found.
func Get(coinID string) *CoinDef {
	if c, ok := Coins[coinID]; ok {
		return c
	}
	return Coins["btc"]
}

// List returns all supported coin IDs in a stable display order.
func List() []string {
	return []string{"btc", "bch", "dgb", "bc2", "xec"}
}
