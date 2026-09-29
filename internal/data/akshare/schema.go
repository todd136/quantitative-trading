package akshare

// JSON schemas emitted by scripts/akshare_fetch.py (stable for Go unmarshal).

type pingResult struct {
	OK             bool    `json:"ok"`
	AKShareVersion *string `json:"akshare_version"`
	Note           string  `json:"note"`
	Error          string  `json:"error"`
}

type errorResult struct {
	Error string `json:"error"`
}

type calendarResult struct {
	Dates  []string `json:"dates"`
	Source string   `json:"source"`
	Error  string   `json:"error"`
}

type securityJSON struct {
	TSCode     string  `json:"ts_code"`
	Name       string  `json:"name"`
	Board      string  `json:"board"`
	ListDate   string  `json:"list_date"`
	DelistDate *string `json:"delist_date"`
}

type securitiesResult struct {
	Securities      []securityJSON `json:"securities"`
	Excluded        int            `json:"excluded_bse_or_unknown"`
	ListDateMissing int            `json:"list_date_missing"`
	ListDateSource  string         `json:"list_date_source"`
	Note            string         `json:"note"`
	Source          string         `json:"source"`
	Error           string         `json:"error"`
}

type barJSON struct {
	TradeDate string  `json:"trade_date"`
	TSCode    string  `json:"ts_code"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
	Amount    float64 `json:"amount"`
	PreClose  float64 `json:"pre_close"`
	AdjFactor float64 `json:"adj_factor"`
	Suspended bool    `json:"suspended"`
}

type barsResult struct {
	Symbol string    `json:"symbol"`
	Bars   []barJSON `json:"bars"`
	Source string    `json:"source"`
	Note   string    `json:"note"`
	Error  string    `json:"error"`
}

type adjRowJSON struct {
	TradeDate string  `json:"trade_date"`
	AdjFactor float64 `json:"adj_factor"`
}

type adjResult struct {
	Symbol string       `json:"symbol"`
	Adj    []adjRowJSON `json:"adj"`
	Source string       `json:"source"`
	Error  string       `json:"error"`
}

type stJSON struct {
	TSCode     string  `json:"ts_code"`
	EntryDate  string  `json:"entry_date"`
	RemoveDate *string `json:"remove_date"`
	STType     string  `json:"st_type"`
}

type recordsResult struct {
	Records []stJSON `json:"records"`
	Note    string   `json:"note"`
	Source  string   `json:"source"`
	Error   string   `json:"error"`
}

type industryJSON struct {
	TSCode        string  `json:"ts_code"`
	IndustryCode  string  `json:"industry_code"`
	IndustryName  string  `json:"industry_name"`
	EffectiveDate string  `json:"effective_date"`
	ExpireDate    *string `json:"expire_date"`
	Source        string  `json:"source"`
}

type industryResult struct {
	Records []industryJSON `json:"records"`
	Note    string         `json:"note"`
	Source  string         `json:"source"`
	Error   string         `json:"error"`
}

type financialJSON struct {
	TSCode           string  `json:"ts_code"`
	ReportPeriod     string  `json:"report_period"`
	AnnouncementDate string  `json:"announcement_date"`
	NetProfit        float64 `json:"net_profit"`
	Revenue          float64 `json:"revenue"`
	GrossProfit      float64 `json:"gross_profit"`
	TotalAssets      float64 `json:"total_assets"`
	TotalLiabilities float64 `json:"total_liabilities"`
	Equity           float64 `json:"equity"`
	OperatingCF      float64 `json:"operating_cf"`
	StatementType    string  `json:"statement_type"`
}

type financialsResult struct {
	Symbol string          `json:"symbol"`
	Rows   []financialJSON `json:"rows"`
	Note   string          `json:"note"`
	Source string          `json:"source"`
	Error  string          `json:"error"`
}

type mvJSON struct {
	TradeDate  string  `json:"trade_date"`
	TSCode     string  `json:"ts_code"`
	TotalShare float64 `json:"total_share"`
	FloatShare float64 `json:"float_share"`
	TotalMV    float64 `json:"total_mv"`
	FloatMV    float64 `json:"float_mv"`
}

type mvResult struct {
	Symbol string   `json:"symbol"`
	Values []mvJSON `json:"values"`
	Note   string   `json:"note"`
	Source string   `json:"source"`
	Error  string   `json:"error"`
}

type indexBarJSON struct {
	TradeDate string  `json:"trade_date"`
	IndexCode string  `json:"index_code"`
	Open      float64 `json:"open"`
	Close     float64 `json:"close"`
}

type indexResult struct {
	IndexCode string         `json:"index_code"`
	Bars      []indexBarJSON `json:"bars"`
	Source    string         `json:"source"`
	Note      string         `json:"note"`
	Error     string         `json:"error"`
}
