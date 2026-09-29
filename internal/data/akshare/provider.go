// Package akshare adapts AKShare (via Python helper or JSON cache) to data.Provider.
//
// Workflow:
//  1. Prefer CacheDir JSON written by FetchAndCache / scripts/akshare_fetch.py --out
//  2. Else invoke Python helper (unless SkipNetwork)
//  3. Network/import failures return typed wrapped errors — never panic
//
// Default CI/tests use fixture.Provider. Set data.provider: akshare in YAML to use this.
package akshare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"quantitative-trading/internal/data"
	"quantitative-trading/internal/types"
)

// Config controls how the adapter reaches AKShare.
type Config struct {
	PythonBin   string // default "python3"
	HelperPath  string // path to scripts/akshare_fetch.py
	HTTPBase    string // optional sidecar base URL (reserved; CLI/cache preferred)
	SkipNetwork bool   // when true, never exec helper; cache-only or ErrSkipped
	CacheDir    string // optional directory of JSON blobs from prior fetches
}

// Provider implements data.Provider using helper CLI and/or CacheDir.
type Provider struct {
	cfg Config
}

var _ data.Provider = (*Provider)(nil)

// New constructs an AKShare adapter.
func New(cfg Config) *Provider {
	if cfg.PythonBin == "" {
		cfg.PythonBin = "python3"
	}
	return &Provider{cfg: cfg}
}

// FetchViaCLI invokes the Python helper with args and returns stdout.
func (p *Provider) FetchViaCLI(args ...string) ([]byte, error) {
	return p.fetchViaCLI(context.Background(), args...)
}

func (p *Provider) fetchViaCLI(ctx context.Context, args ...string) ([]byte, error) {
	if p.cfg.SkipNetwork {
		return nil, ErrSkipped
	}
	if p.cfg.HelperPath == "" {
		return nil, fmt.Errorf("%w: HelperPath not configured", ErrHelperFailed)
	}
	cmdArgs := append([]string{p.cfg.HelperPath}, args...)
	cmd := exec.CommandContext(ctx, p.cfg.PythonBin, cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.Bytes()
	errText := strings.TrimSpace(stderr.String())
	if err != nil {
		msg := errText
		if msg == "" {
			msg = string(out)
		}
		if strings.Contains(msg, "pip install akshare") || strings.Contains(msg, "No module named 'akshare'") {
			return out, fmt.Errorf("%w: %s", ErrAKShareImport, msg)
		}
		var er errorResult
		if json.Unmarshal(out, &er) == nil && er.Error != "" {
			msg = er.Error
		} else if json.Unmarshal([]byte(errText), &er) == nil && er.Error != "" {
			msg = er.Error
		}
		return out, wrapHelper(err, msg)
	}
	return out, nil
}

// FetchAndCache runs the helper and writes JSON under CacheDir using a stable key.
func (p *Provider) FetchAndCache(ctx context.Context, cacheKey string, args ...string) ([]byte, error) {
	out, err := p.fetchViaCLI(ctx, args...)
	if err != nil {
		return out, err
	}
	if p.cfg.CacheDir != "" && cacheKey != "" {
		if werr := p.writeCache(cacheKey, out); werr != nil {
			return out, fmt.Errorf("cache write: %w", werr)
		}
	}
	return out, nil
}

func (p *Provider) cachePath(key string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, key)
	return filepath.Join(p.cfg.CacheDir, safe+".json")
}

func (p *Provider) writeCache(key string, raw []byte) error {
	if err := os.MkdirAll(p.cfg.CacheDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(p.cachePath(key), raw, 0o644)
}

func (p *Provider) readCache(key string) ([]byte, error) {
	if p.cfg.CacheDir == "" {
		return nil, ErrNotCached
	}
	b, err := os.ReadFile(p.cachePath(key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotCached
		}
		return nil, err
	}
	return b, nil
}

// loadJSON tries cache first, then helper (unless SkipNetwork).
func (p *Provider) loadJSON(ctx context.Context, cacheKey string, helperArgs ...string) ([]byte, error) {
	if b, err := p.readCache(cacheKey); err == nil {
		return b, nil
	} else if err != ErrNotCached {
		return nil, err
	}
	if p.cfg.SkipNetwork {
		return nil, ErrSkipped
	}
	return p.FetchAndCache(ctx, cacheKey, helperArgs...)
}

// loadJSONRetry retries transient helper/network failures (Eastmoney RemoteDisconnected etc.).
func (p *Provider) loadJSONRetry(ctx context.Context, attempts int, cacheKey string, helperArgs ...string) ([]byte, error) {
	if attempts < 1 {
		attempts = 1
	}
	var last error
	var out []byte
	for i := 0; i < attempts; i++ {
		out, last = p.loadJSON(ctx, cacheKey, helperArgs...)
		if last == nil {
			return out, nil
		}
		// Cache miss with SkipNetwork / not cached — do not retry.
		if last == ErrSkipped || last == ErrNotCached || last == ErrAKShareImport {
			return out, last
		}
		if i+1 >= attempts {
			break
		}
		msg := last.Error()
		transient := strings.Contains(msg, "RemoteDisconnected") ||
			strings.Contains(msg, "Connection") ||
			strings.Contains(msg, "Timeout") ||
			strings.Contains(msg, "timed out") ||
			strings.Contains(msg, "EOF") ||
			strings.Contains(msg, "reset by peer") ||
			strings.Contains(msg, "Temporary") ||
			strings.Contains(msg, "helper failed")
		if !transient {
			return out, last
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(time.Duration(1<<i) * time.Second):
		}
	}
	return out, last
}

func parseDate(s string) (types.TradeDate, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return types.TradeDate{}, fmt.Errorf("empty date")
	}
	return types.ParseTradeDate(s)
}

func optDate(s *string) (*types.TradeDate, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, nil
	}
	d, err := parseDate(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func inRange(d, from, to types.TradeDate) bool {
	if !from.Time().IsZero() && d.Before(from) {
		return false
	}
	if !to.Time().IsZero() && d.After(to) {
		return false
	}
	return true
}

// LoadCalendar implements data.Provider.
func (p *Provider) LoadCalendar() ([]types.TradeDate, error) {
	raw, err := p.loadJSON(context.Background(), "calendar", "calendar")
	if err != nil {
		return nil, err
	}
	var res calendarResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, wrapHelper(err, "calendar json")
	}
	if res.Error != "" {
		return nil, wrapHelper(nil, res.Error)
	}
	out := make([]types.TradeDate, 0, len(res.Dates))
	for _, s := range res.Dates {
		d, err := parseDate(s)
		if err != nil {
			return nil, fmt.Errorf("calendar date %q: %w", s, err)
		}
		out = append(out, d)
	}
	return out, nil
}

// Securities implements data.Provider. BSE codes are filtered out.
func (p *Provider) Securities() ([]types.SecurityMaster, error) {
	raw, err := p.loadJSON(context.Background(), "securities", "securities")
	if err != nil {
		return nil, err
	}
	var res securitiesResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, wrapHelper(err, "securities json")
	}
	if res.Error != "" {
		return nil, wrapHelper(nil, res.Error)
	}
	out := make([]types.SecurityMaster, 0, len(res.Securities))
	for _, s := range res.Securities {
		if IsBSE(s.TSCode) {
			continue
		}
		board, ok := MapBoard(s.TSCode)
		if !ok {
			continue
		}
		if s.Board != "" {
			board = types.Board(s.Board)
			if board == types.BoardBSE {
				continue
			}
		}
		sm := types.SecurityMaster{
			TSCode: types.SecurityID(s.TSCode),
			Name:   s.Name,
			Board:  board,
		}
		if s.ListDate != "" {
			ld, err := parseDate(s.ListDate)
			if err != nil {
				return nil, err
			}
			sm.ListDate = ld
		}
		dd, err := optDate(s.DelistDate)
		if err != nil {
			return nil, err
		}
		sm.DelistDate = dd
		out = append(out, sm)
	}
	return out, nil
}

// Bars implements data.Provider. Merges adj factors when available.
func (p *Provider) Bars(tsCode types.SecurityID, from, to types.TradeDate) ([]types.Bar, error) {
	start := "1990-01-01"
	end := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	if !from.Time().IsZero() {
		start = from.String()
	}
	if !to.Time().IsZero() {
		end = to.String()
	}
	// Include window in cache key so date-scoped fetches do not reuse full-history blobs.
	key := fmt.Sprintf("bars_%s_%s_%s", string(tsCode), start, end)
	args := []string{"bars", "--symbol", string(tsCode), "--start", start, "--end", end}
	raw, err := p.loadJSON(context.Background(), key, args...)
	if err != nil {
		return nil, err
	}
	var res barsResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, wrapHelper(err, "bars json")
	}
	if res.Error != "" {
		return nil, wrapHelper(nil, res.Error)
	}

	adjMap := map[string]float64{}
	adjKey := "adj_" + string(tsCode)
	// Date-scoped + CacheDir: use cached adj only — skip a second full-history
	// network pull per symbol (bars carry adj_factor, defaulting to 1.0).
	// Without CacheDir (unit tests / mock helper), still invoke the adj helper.
	windowed := !from.Time().IsZero() && !to.Time().IsZero()
	var adjRaw []byte
	var aerr error
	if windowed && p.cfg.CacheDir != "" {
		adjRaw, aerr = p.readCache(adjKey)
		if aerr == ErrNotCached {
			aerr = nil
			adjRaw = nil
		}
	} else {
		adjRaw, aerr = p.loadJSON(context.Background(), adjKey, "adj", "--symbol", string(tsCode))
	}
	if aerr == nil && len(adjRaw) > 0 {
		var ar adjResult
		if json.Unmarshal(adjRaw, &ar) == nil {
			for _, r := range ar.Adj {
				adjMap[r.TradeDate] = r.AdjFactor
			}
		}
	} else if aerr != nil && aerr != ErrSkipped && aerr != ErrNotCached {
		// Non-fatal: bars still usable with adj_factor from bar payload / 1.0
		_ = aerr
	}

	out := make([]types.Bar, 0, len(res.Bars))
	for _, b := range res.Bars {
		d, err := parseDate(b.TradeDate)
		if err != nil {
			return nil, err
		}
		if !inRange(d, from, to) {
			continue
		}
		adj := b.AdjFactor
		if adj == 0 {
			adj = 1
		}
		if v, ok := adjMap[b.TradeDate]; ok && v != 0 {
			adj = v
		}
		code := b.TSCode
		if code == "" {
			code = string(tsCode)
		}
		out = append(out, types.Bar{
			TradeDate: d,
			TSCode:    types.SecurityID(code),
			Open:      b.Open,
			High:      b.High,
			Low:       b.Low,
			Close:     b.Close,
			Volume:    b.Volume,
			Amount:    b.Amount,
			PreClose:  b.PreClose,
			AdjFactor: adj,
			Suspended: b.Suspended || b.Volume == 0,
		})
	}
	return out, nil
}

// ST implements data.Provider (best-effort snapshot; may be empty with note in helper).
func (p *Provider) ST() ([]types.STRecord, error) {
	raw, err := p.loadJSONRetry(context.Background(), 3, "st", "st")
	if err != nil {
		return nil, err
	}
	var res recordsResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, wrapHelper(err, "st json")
	}
	if res.Error != "" {
		return nil, wrapHelper(nil, res.Error)
	}
	out := make([]types.STRecord, 0, len(res.Records))
	for _, r := range res.Records {
		if IsBSE(r.TSCode) {
			continue
		}
		ed, err := parseDate(r.EntryDate)
		if err != nil {
			return nil, err
		}
		rd, err := optDate(r.RemoveDate)
		if err != nil {
			return nil, err
		}
		out = append(out, types.STRecord{
			TSCode:     types.SecurityID(r.TSCode),
			EntryDate:  ed,
			RemoveDate: rd,
			STType:     r.STType,
		})
	}
	return out, nil
}

// Industry implements data.Provider (best-effort).
func (p *Provider) Industry() ([]types.IndustryRecord, error) {
	raw, err := p.loadJSONRetry(context.Background(), 3, "industry", "industry")
	if err != nil {
		return nil, err
	}
	var res industryResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, wrapHelper(err, "industry json")
	}
	if res.Error != "" {
		return nil, wrapHelper(nil, res.Error)
	}
	if res.Note != "" {
		fmt.Fprintf(os.Stderr, "akshare industry note: %s (source=%s)\n", res.Note, res.Source)
	}
	out := make([]types.IndustryRecord, 0, len(res.Records))
	for _, r := range res.Records {
		if IsBSE(r.TSCode) {
			continue
		}
		if strings.TrimSpace(r.IndustryCode) == "" && strings.TrimSpace(r.IndustryName) == "" {
			continue // never invent industry membership
		}
		ed, err := parseDate(r.EffectiveDate)
		if err != nil {
			// tolerate missing — use epoch; helper notes the gap
			ed, _ = types.ParseTradeDate("2000-01-01")
		}
		xd, err := optDate(r.ExpireDate)
		if err != nil {
			return nil, err
		}
		src := r.Source
		if src == "" {
			src = "EM_INDUSTRY"
		}
		out = append(out, types.IndustryRecord{
			TSCode:        types.SecurityID(r.TSCode),
			IndustryCode:  r.IndustryCode,
			IndustryName:  r.IndustryName,
			EffectiveDate: ed,
			ExpireDate:    xd,
			Source:        src,
		})
	}
	if len(out) == 0 {
		fmt.Fprintf(os.Stderr, "akshare industry: empty membership (note=%q)\n", res.Note)
	}
	return out, nil
}

// Financials implements data.Provider (best-effort; may be empty).
// Rows missing equity/revenue/total_assets (treated as zero upstream) are dropped —
// never surface fabricated zeros as valid fundamentals for Quality/Value.
func (p *Provider) Financials(tsCode types.SecurityID) ([]types.FinancialRow, error) {
	key := "financials_" + string(tsCode)
	raw, err := p.loadJSON(context.Background(), key, "financials", "--symbol", string(tsCode))
	if err != nil {
		return nil, err
	}
	var res financialsResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, wrapHelper(err, "financials json")
	}
	if res.Error != "" {
		return nil, wrapHelper(nil, res.Error)
	}
	if res.Note != "" {
		fmt.Fprintf(os.Stderr, "akshare financials %s note: %s (source=%s)\n", tsCode, res.Note, res.Source)
	}
	out := make([]types.FinancialRow, 0, len(res.Rows))
	skippedInvalid := 0
	for _, r := range res.Rows {
		rp, err := parseDate(r.ReportPeriod)
		if err != nil {
			continue
		}
		// Reject incomplete fundamentals (helper should already omit these).
		if r.Equity == 0 || r.Revenue == 0 || r.TotalAssets == 0 {
			skippedInvalid++
			continue
		}
		ad := rp
		if r.AnnouncementDate != "" {
			if d, err := parseDate(r.AnnouncementDate); err == nil {
				ad = d
			}
		}
		out = append(out, types.FinancialRow{
			TSCode:           types.SecurityID(r.TSCode),
			ReportPeriod:     rp,
			AnnouncementDate: ad,
			NetProfit:        r.NetProfit,
			Revenue:          r.Revenue,
			GrossProfit:      r.GrossProfit,
			TotalAssets:      r.TotalAssets,
			TotalLiabilities: r.TotalLiabilities,
			Equity:           r.Equity,
			OperatingCF:      r.OperatingCF,
			StatementType:    r.StatementType,
		})
	}
	if skippedInvalid > 0 {
		fmt.Fprintf(os.Stderr,
			"akshare financials %s: skipped %d rows with missing equity/revenue/assets (not treating 0 as valid)\n",
			tsCode, skippedInvalid)
	}
	return out, nil
}

// MarketValues implements data.Provider (best-effort snapshot / series).
// Entries with TotalMV<=0 are dropped — missing MV is not a valid zero.
func (p *Provider) MarketValues(tsCode types.SecurityID, from, to types.TradeDate) ([]types.MarketValue, error) {
	key := "mv_" + string(tsCode)
	args := []string{"mv", "--symbol", string(tsCode)}
	if !from.Time().IsZero() {
		args = append(args, "--start", from.String())
	}
	if !to.Time().IsZero() {
		args = append(args, "--end", to.String())
	}
	raw, err := p.loadJSON(context.Background(), key, args...)
	if err != nil {
		return nil, err
	}
	var res mvResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, wrapHelper(err, "mv json")
	}
	if res.Error != "" {
		return nil, wrapHelper(nil, res.Error)
	}
	if res.Note != "" {
		fmt.Fprintf(os.Stderr, "akshare mv %s note: %s (source=%s)\n", tsCode, res.Note, res.Source)
	}
	out := make([]types.MarketValue, 0, len(res.Values))
	skipped := 0
	for _, m := range res.Values {
		d, err := parseDate(m.TradeDate)
		if err != nil {
			continue
		}
		if !inRange(d, from, to) {
			continue
		}
		if m.TotalMV <= 0 {
			skipped++
			continue
		}
		out = append(out, types.MarketValue{
			TradeDate:  d,
			TSCode:     types.SecurityID(m.TSCode),
			TotalShare: m.TotalShare,
			FloatShare: m.FloatShare,
			TotalMV:    m.TotalMV,
			FloatMV:    m.FloatMV,
		})
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "akshare mv %s: skipped %d rows with total_mv<=0\n", tsCode, skipped)
	}
	return out, nil
}

// IndexBars implements data.Provider.
func (p *Provider) IndexBars(indexCode string, from, to types.TradeDate) ([]types.IndexBar, error) {
	start := "1990-01-01"
	end := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	if !from.Time().IsZero() {
		start = from.String()
	}
	if !to.Time().IsZero() {
		end = to.String()
	}
	key := fmt.Sprintf("index_%s_%s_%s", indexCode, start, end)
	args := []string{"index", "--symbol", indexCode, "--start", start, "--end", end}
	raw, err := p.loadJSONRetry(context.Background(), 3, key, args...)
	if err != nil {
		return nil, err
	}
	var res indexResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, wrapHelper(err, "index json")
	}
	if res.Error != "" {
		return nil, wrapHelper(nil, res.Error)
	}
	out := make([]types.IndexBar, 0, len(res.Bars))
	for _, b := range res.Bars {
		d, err := parseDate(b.TradeDate)
		if err != nil {
			continue
		}
		if !inRange(d, from, to) {
			continue
		}
		code := b.IndexCode
		if code == "" {
			code = indexCode
		}
		out = append(out, types.IndexBar{
			TradeDate: d,
			IndexCode: code,
			Open:      b.Open,
			Close:     b.Close,
		})
	}
	return out, nil
}

// PublicTexts is not provided by AKShare; returns empty (or ErrSkipped when offline with no cache).
func (p *Provider) PublicTexts(tsCode types.SecurityID, asofEnd types.TradeDate) ([]types.PublicText, error) {
	if p.cfg.SkipNetwork {
		return nil, ErrSkipped
	}
	return nil, nil
}
