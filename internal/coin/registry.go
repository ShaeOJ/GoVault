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
	"btcs": {
		// Real BitcoinSilver params (the original seed was a wrong Bitcoin
		// placeholder — "Bitcoin Satoshi", HRP "bc", version 0x00). Legacy
		// addresses are "B..." (P2PKH version 0x1a / 26), bech32 is "bs1q...".
		Name:               "Bitcoin Silver",
		Symbol:             "BTCS",
		CoinID:             "btcs",
		SegWit:             true,
		Bech32HRP:          "bs",
		P2PKHVersion:       0x1a,
		P2SHVersion:        0x05,
		P2PKHPrefixChar:    'B',
		P2SHPrefixChar:     '3',
		DefaultRPCPort:     8351,
		DefaultRPCUsername: "silveroj",
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
	"ltc": {
		Name:               "Litecoin",
		Symbol:             "LTC",
		CoinID:             "ltc",
		SegWit:             true,
		Bech32HRP:          "ltc",
		P2PKHVersion:       0x30,
		P2SHVersion:        0x32,
		P2PKHPrefixChar:    'L',
		P2SHPrefixChar:     'M',
		DefaultRPCPort:     9332,
		DefaultRPCUsername: "litecoin",
		GBTRules:           []string{"segwit"},
		TargetBlockTimeSec: 150,
	},
	"bch2": {
		Name:               "Bitcoin Cash II",
		Symbol:             "BCH2",
		CoinID:             "bch2",
		SegWit:             false,
		CashAddrPrefix:     "bitcoincashii",
		P2PKHVersion:       0x00,
		P2SHVersion:        0x05,
		DefaultRPCPort:     8341,
		DefaultRPCUsername: "bitcoincashii",
		GBTRules:           []string{},
		TargetBlockTimeSec: 600,
	},
	"fix": {
		Name:               "FixedCoin",
		Symbol:             "FIX",
		CoinID:             "fix",
		SegWit:             true,
		Bech32HRP:          "fix",
		P2PKHVersion:       0x01,
		P2SHVersion:        0x00,
		// FIX legacy P2PKH (version byte 0x01) addresses start with either 'R'
		// or 'h'. ValidateAddress gates on the first char, so it recognises the
		// common 'R...' form. Passthrough mode forwards the worker to upstream
		// without local validation, so this only matters for solo mode.
		P2PKHPrefixChar:    'R',
		DefaultRPCPort:     24761,
		DefaultRPCUsername: "fixrpc",
		GBTRules:           []string{"segwit"},
		TargetBlockTimeSec: 600,
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
	return []string{"btc", "bch", "dgb", "bc2", "btcs", "xec", "ltc", "bch2", "fix"}
}
