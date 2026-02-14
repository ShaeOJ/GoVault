package coin

// CoinDef holds all per-coin parameters needed by the stratum server.
type CoinDef struct {
	// Identity
	Name   string // "Bitcoin", "Bitcoin Cash", "DigiByte", "Bitcoin II", "eCash"
	Symbol string // "BTC", "BCH", "DGB", "BC2", "XEC"
	CoinID string // config key: "btc", "bch", "dgb", "bc2", "xec"

	// SegWit support
	SegWit bool // true for BTC, DGB, BC2; false for BCH, XEC

	// Address encoding
	Bech32HRP      string // "bc" for BTC/BC2, "dgb" for DGB, "" for CashAddr coins
	CashAddrPrefix string // "bitcoincash" for BCH, "ecash" for XEC, "" otherwise

	// Base58check version bytes
	P2PKHVersion    byte // 0x00 BTC/BC2, 0x1e DGB
	P2SHVersion     byte // 0x05 BTC/BC2, 0x3f DGB
	P2PKHPrefixChar byte // '1' BTC/BC2, 'D' DGB
	P2SHPrefixChar  byte // '3' BTC/BC2, 'S' DGB

	// Node RPC defaults
	DefaultRPCPort     int      // 8332, 14022, etc.
	DefaultRPCUsername string   // "bitcoin", "digibyte", etc.
	GBTRules           []string // ["segwit"] for SegWit coins, [] for non-SegWit

	// Block parameters
	TargetBlockTimeSec int // 600 for most, 60 for DGB

	// XEC-specific mandatory coinbase outputs
	HasMinerFund     bool // true only for XEC
	HasStakingReward bool // true only for XEC
}
