// Package types holds shared domain types for the A-share multifactor backtest skeleton.
package types

import (
	"time"
)

// Board identifies an exchange board segment.
type Board string

const (
	BoardSSEMain  Board = "SSE_MAIN"
	BoardSZSEMain Board = "SZSE_MAIN"
	BoardChiNext  Board = "CHINEXT"
	BoardSTAR     Board = "STAR"
	BoardBSE      Board = "BSE" // Beijing Exchange — excluded by default
)

// SecurityID is the unified instrument code (e.g. "600000.SH").
type SecurityID string

// TradeDate is a calendar trading day (date only, Asia/Shanghai).
type TradeDate time.Time

func NewTradeDate(y int, m time.Month, d int) TradeDate {
	return TradeDate(time.Date(y, m, d, 0, 0, 0, 0, time.FixedZone("CST", 8*3600)))
}

func (t TradeDate) Time() time.Time { return time.Time(t) }

func (t TradeDate) String() string { return time.Time(t).Format("2006-01-02") }

func (t TradeDate) Equal(o TradeDate) bool {
	a, b := time.Time(t), time.Time(o)
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func (t TradeDate) Before(o TradeDate) bool { return time.Time(t).Before(time.Time(o)) }

func (t TradeDate) After(o TradeDate) bool { return time.Time(t).After(time.Time(o)) }

// ParseTradeDate parses YYYY-MM-DD in Asia/Shanghai.
func ParseTradeDate(s string) (TradeDate, error) {
	loc := time.FixedZone("CST", 8*3600)
	tm, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return TradeDate{}, err
	}
	return TradeDate(tm), nil
}

// Bar is one day of OHLCV + limit / suspend flags.
type Bar struct {
	TradeDate   TradeDate
	TSCode      SecurityID
	Open        float64
	High        float64
	Low         float64
	Close       float64
	Volume      float64
	Amount      float64 // CNY
	PreClose    float64
	HighLimit   float64
	LowLimit    float64
	AdjFactor   float64
	Suspended   bool
	LimitStatus string // optional vendor enum
}

// CloseAdj returns close * adj_factor.
func (b Bar) CloseAdj() float64 {
	if b.AdjFactor == 0 {
		return b.Close
	}
	return b.Close * b.AdjFactor
}

// SecurityMaster is static / slowly changing instrument metadata.
type SecurityMaster struct {
	TSCode     SecurityID
	Name       string
	Board      Board
	ListDate   TradeDate
	DelistDate *TradeDate // nil if still listed
}

// STRecord covers an ST / *ST interval (PIT).
type STRecord struct {
	TSCode     SecurityID
	EntryDate  TradeDate
	RemoveDate *TradeDate // nil = still ST
	STType     string
	// PITBackfill is true when entry_date was stamped (e.g. 1990-01-01) because
	// the vendor only provided a current ST snapshot. Strict PIT rejects these.
	PITBackfill bool
}

// IndustryRecord is point-in-time industry membership.
type IndustryRecord struct {
	TSCode        SecurityID
	IndustryCode  string
	IndustryName  string
	EffectiveDate TradeDate
	ExpireDate    *TradeDate
	Source        string // SW_L1 / CITIC_L1
	// PITBackfill is true when effective_date was stamped without a historical
	// membership interval. Strict PIT rejects these.
	PITBackfill bool
}

// FinancialRow is one financial statement snapshot keyed by announcement date.
type FinancialRow struct {
	TSCode           SecurityID
	ReportPeriod     TradeDate
	AnnouncementDate TradeDate
	NetProfit        float64
	Revenue          float64
	GrossProfit      float64 // or revenue - operating_cost
	TotalAssets      float64
	TotalLiabilities float64
	Equity           float64
	OperatingCF      float64
	StatementType    string // consolidated default
	// AnnouncementIsProxy is true when announcement_date was filled with report_period
	// (or otherwise invented). Strict PIT must reject these rows.
	AnnouncementIsProxy bool
}

// MarketValue is daily market-cap / share data.
type MarketValue struct {
	TradeDate  TradeDate
	TSCode     SecurityID
	TotalShare float64
	FloatShare float64
	TotalMV    float64
	FloatMV    float64
	// IsSnapshot marks a current-day quote stamped onto a historical date.
	// Strict PIT must reject these; even non-strict lookup skips them.
	IsSnapshot bool
	Source     string
}

// IndexBar is benchmark EOD.
type IndexBar struct {
	TradeDate TradeDate
	IndexCode string
	Open      float64
	Close     float64
}

// FactorScores holds processed Z scores for one name on one day.
type FactorScores struct {
	TSCode   SecurityID
	Quality  *float64
	Value    *float64
	Momentum *float64
	LLM      *float64
}

// CompositeResult is the ranked score after synthesis.
type CompositeResult struct {
	TSCode    SecurityID
	Composite float64
	Industry  string
}

// TargetHolding is equal-weight (or other) target after SelectHoldings.
type TargetHolding struct {
	TSCode SecurityID
	Weight float64
}

// Lot tracks T+1 available quantity.
type Lot struct {
	TSCode        SecurityID
	Shares        float64
	AvailableFrom TradeDate // earliest sell date (next trading day after buy)
	CostBasis     float64
}

// Fill is an executed trade.
type Fill struct {
	TradeDate TradeDate
	TSCode    SecurityID
	Side      string // BUY / SELL
	Price     float64
	Shares    float64
	Amount    float64
	Cost      float64 // commission + stamp + slippage
}

// ValuationMark records how a holding was priced when the session close was not used.
type ValuationMark struct {
	Price  float64
	AsOf   TradeDate // date of the price that was carried
	Source string    // session_close | last_close
	Reason string    // confirmed_halt | missing_bar
}

// DailySnapshot is end-of-day portfolio state for persistence.
type DailySnapshot struct {
	TradeDate TradeDate
	NAV       float64
	Cash      float64
	// Receivable is confirmed cash dividend not yet paid (ex-date through cash-pay date).
	Receivable float64
	Holdings   map[SecurityID]float64 // shares
	Weights    map[SecurityID]float64
	Turnover   float64
	CostTotal  float64
	// HaltValued lists holdings marked to last valid price because the name was
	// a confirmed halt (bar.Suspended or suspend calendar). Empty when none.
	HaltValued map[SecurityID]float64
	// MissingValued lists holdings marked to last valid price because the session
	// bar is missing and halt was not confirmed. Distinct from HaltValued.
	MissingValued map[SecurityID]float64
	// CarryMarks is the per-name audit trail (price / as-of / source / reason).
	CarryMarks map[SecurityID]ValuationMark
}

// CorpAction is one announced corporate action. Dates are calendar dates (not inferred
// from adj_factor). Kind must be a supported enumerator or the engine fail-closes.
type CorpAction struct {
	TSCode             SecurityID
	Kind               string // profit_distribution | cash_dividend | bonus_share | capital_transfer | unsupported
	AnnouncementDate   TradeDate
	RecordDate         TradeDate // 股权登记日
	ExDate             TradeDate // 除权除息日
	CashPayDate        TradeDate // 现金红利发放日; zero → same as ExDate
	SharesTradableDate TradeDate // 新增股份上市/可交易日; zero → same as ExDate
	CashPerShare       float64   // 税前每股派现
	BonusRatio         float64   // 每股送股
	TransferRatio      float64   // 每股转增
	UnsupportedReason  string
}

// PublicText is a publicly available text document for optional LLM assist (PIT via AsofTS).
type PublicText struct {
	SourceID   string
	TSCode     SecurityID // may be empty for macro/industry text
	AsofTS     time.Time  // Asia/Shanghai; must be ≤ end of signal day T
	SourceType string
	TextRef    string // storage ref or content hash; logs must not dump full text by default
	Lang       string
}

// LlmFeature is the auditable LLM plugin output schema (§9.5).
type LlmFeature struct {
	TSCode         SecurityID
	SignalDate     TradeDate
	SentimentScore *float64
	EventTags      []string
	Confidence     float64
	SourceIDs      []string
	AsofTS         time.Time
	ModelID        string
	PromptVersion  string
	CacheKey       string
}
