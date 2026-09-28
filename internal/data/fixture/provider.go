// Package fixture implements data.Provider by reading CSV/JSON under testdata/fixtures.
package fixture

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"quantitative-trading/internal/data"
	"quantitative-trading/internal/types"
)

// Provider loads synthetic or checked-in fixtures from a directory.
type Provider struct {
	dir        string
	calendar   []types.TradeDate
	securities []types.SecurityMaster
	bars       map[types.SecurityID][]types.Bar
	st         []types.STRecord
	industry   []types.IndustryRecord
	financials map[types.SecurityID][]types.FinancialRow
	mvs        map[types.SecurityID][]types.MarketValue
	index      map[string][]types.IndexBar
	texts      []types.PublicText
}

var _ data.Provider = (*Provider)(nil)

// New loads all fixture files from dir.
func New(dir string) (*Provider, error) {
	p := &Provider{
		dir:        dir,
		bars:       make(map[types.SecurityID][]types.Bar),
		financials: make(map[types.SecurityID][]types.FinancialRow),
		mvs:        make(map[types.SecurityID][]types.MarketValue),
		index:      make(map[string][]types.IndexBar),
	}
	if err := p.loadAll(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Provider) loadAll() error {
	var err error
	if p.calendar, err = p.loadCalendar(); err != nil {
		return err
	}
	if p.securities, err = p.loadSecurities(); err != nil {
		return err
	}
	if err = p.loadBars(); err != nil {
		return err
	}
	if p.st, err = p.loadST(); err != nil {
		return err
	}
	if p.industry, err = p.loadIndustry(); err != nil {
		return err
	}
	if err = p.loadFinancials(); err != nil {
		return err
	}
	if err = p.loadMVs(); err != nil {
		return err
	}
	if err = p.loadIndex(); err != nil {
		return err
	}
	if p.texts, err = p.loadTexts(); err != nil {
		return err
	}
	return nil
}

func (p *Provider) path(name string) string { return filepath.Join(p.dir, name) }

func readCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 1 {
		return nil, fmt.Errorf("empty csv: %s", path)
	}
	return rows, nil
}

func parseDate(s string) (types.TradeDate, error) {
	return types.ParseTradeDate(strings.TrimSpace(s))
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseBool(s string) bool {
	s = strings.TrimSpace(s)
	return s == "1" || strings.EqualFold(s, "true") || s == "Y"
}

func (p *Provider) loadCalendar() ([]types.TradeDate, error) {
	rows, err := readCSV(p.path("calendar.csv"))
	if err != nil {
		return nil, err
	}
	out := make([]types.TradeDate, 0, len(rows)-1)
	for _, row := range rows[1:] {
		if len(row) < 3 || !parseBool(row[2]) {
			continue
		}
		d, err := parseDate(row[0])
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (p *Provider) loadSecurities() ([]types.SecurityMaster, error) {
	rows, err := readCSV(p.path("securities.csv"))
	if err != nil {
		return nil, err
	}
	out := make([]types.SecurityMaster, 0, len(rows)-1)
	for _, row := range rows[1:] {
		ld, err := parseDate(row[3])
		if err != nil {
			return nil, err
		}
		sm := types.SecurityMaster{
			TSCode:   types.SecurityID(row[0]),
			Name:     row[1],
			Board:    types.Board(row[2]),
			ListDate: ld,
		}
		if strings.TrimSpace(row[4]) != "" {
			dd, err := parseDate(row[4])
			if err != nil {
				return nil, err
			}
			sm.DelistDate = &dd
		}
		out = append(out, sm)
	}
	return out, nil
}

func (p *Provider) loadBars() error {
	rows, err := readCSV(p.path("bars.csv"))
	if err != nil {
		return err
	}
	for _, row := range rows[1:] {
		d, err := parseDate(row[0])
		if err != nil {
			return err
		}
		b := types.Bar{
			TradeDate:   d,
			TSCode:      types.SecurityID(row[1]),
			Open:        parseFloat(row[2]),
			High:        parseFloat(row[3]),
			Low:         parseFloat(row[4]),
			Close:       parseFloat(row[5]),
			Volume:      parseFloat(row[6]),
			Amount:      parseFloat(row[7]),
			PreClose:    parseFloat(row[8]),
			HighLimit:   parseFloat(row[9]),
			LowLimit:    parseFloat(row[10]),
			AdjFactor:   parseFloat(row[11]),
			Suspended:   parseBool(row[12]),
			LimitStatus: row[13],
		}
		p.bars[b.TSCode] = append(p.bars[b.TSCode], b)
	}
	return nil
}

func (p *Provider) loadST() ([]types.STRecord, error) {
	rows, err := readCSV(p.path("st.csv"))
	if err != nil {
		return nil, err
	}
	out := make([]types.STRecord, 0, len(rows)-1)
	for _, row := range rows[1:] {
		ed, err := parseDate(row[1])
		if err != nil {
			return nil, err
		}
		rec := types.STRecord{
			TSCode:    types.SecurityID(row[0]),
			EntryDate: ed,
			STType:    row[3],
		}
		if strings.TrimSpace(row[2]) != "" {
			rd, err := parseDate(row[2])
			if err != nil {
				return nil, err
			}
			rec.RemoveDate = &rd
		}
		out = append(out, rec)
	}
	return out, nil
}

func (p *Provider) loadIndustry() ([]types.IndustryRecord, error) {
	rows, err := readCSV(p.path("industry.csv"))
	if err != nil {
		return nil, err
	}
	out := make([]types.IndustryRecord, 0, len(rows)-1)
	for _, row := range rows[1:] {
		ed, err := parseDate(row[3])
		if err != nil {
			return nil, err
		}
		rec := types.IndustryRecord{
			TSCode:        types.SecurityID(row[0]),
			IndustryCode:  row[1],
			IndustryName:  row[2],
			EffectiveDate: ed,
			Source:        row[5],
		}
		if strings.TrimSpace(row[4]) != "" {
			xd, err := parseDate(row[4])
			if err != nil {
				return nil, err
			}
			rec.ExpireDate = &xd
		}
		out = append(out, rec)
	}
	return out, nil
}

func (p *Provider) loadFinancials() error {
	rows, err := readCSV(p.path("financials.csv"))
	if err != nil {
		return err
	}
	for _, row := range rows[1:] {
		rp, err := parseDate(row[1])
		if err != nil {
			return err
		}
		ad, err := parseDate(row[2])
		if err != nil {
			return err
		}
		fr := types.FinancialRow{
			TSCode:           types.SecurityID(row[0]),
			ReportPeriod:     rp,
			AnnouncementDate: ad,
			NetProfit:        parseFloat(row[3]),
			Revenue:          parseFloat(row[4]),
			GrossProfit:      parseFloat(row[5]),
			TotalAssets:      parseFloat(row[6]),
			TotalLiabilities: parseFloat(row[7]),
			Equity:           parseFloat(row[8]),
			OperatingCF:      parseFloat(row[9]),
			StatementType:    row[10],
		}
		p.financials[fr.TSCode] = append(p.financials[fr.TSCode], fr)
	}
	return nil
}

func (p *Provider) loadMVs() error {
	rows, err := readCSV(p.path("market_values.csv"))
	if err != nil {
		return err
	}
	for _, row := range rows[1:] {
		d, err := parseDate(row[0])
		if err != nil {
			return err
		}
		mv := types.MarketValue{
			TradeDate:  d,
			TSCode:     types.SecurityID(row[1]),
			TotalShare: parseFloat(row[2]),
			FloatShare: parseFloat(row[3]),
			TotalMV:    parseFloat(row[4]),
			FloatMV:    parseFloat(row[5]),
		}
		p.mvs[mv.TSCode] = append(p.mvs[mv.TSCode], mv)
	}
	return nil
}

func (p *Provider) loadIndex() error {
	rows, err := readCSV(p.path("index_bars.csv"))
	if err != nil {
		return err
	}
	for _, row := range rows[1:] {
		d, err := parseDate(row[0])
		if err != nil {
			return err
		}
		ib := types.IndexBar{
			TradeDate: d,
			IndexCode: row[1],
			Open:      parseFloat(row[2]),
			Close:     parseFloat(row[3]),
		}
		p.index[ib.IndexCode] = append(p.index[ib.IndexCode], ib)
	}
	return nil
}

func (p *Provider) loadTexts() ([]types.PublicText, error) {
	path := p.path("public_texts.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var raw []struct {
		SourceID   string `json:"source_id"`
		TSCode     string `json:"ts_code"`
		AsofTS     string `json:"asof_ts"`
		SourceType string `json:"source_type"`
		TextRef    string `json:"text_ref"`
		Lang       string `json:"lang"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	loc := time.FixedZone("CST", 8*3600)
	out := make([]types.PublicText, 0, len(raw))
	for _, r := range raw {
		ts, err := time.Parse(time.RFC3339, r.AsofTS)
		if err != nil {
			ts, err = time.ParseInLocation("2006-01-02T15:04:05", r.AsofTS, loc)
			if err != nil {
				return nil, fmt.Errorf("asof_ts %q: %w", r.AsofTS, err)
			}
		}
		out = append(out, types.PublicText{
			SourceID:   r.SourceID,
			TSCode:     types.SecurityID(r.TSCode),
			AsofTS:     ts,
			SourceType: r.SourceType,
			TextRef:    r.TextRef,
			Lang:       r.Lang,
		})
	}
	return out, nil
}

func (p *Provider) LoadCalendar() ([]types.TradeDate, error) {
	return append([]types.TradeDate(nil), p.calendar...), nil
}

func (p *Provider) Securities() ([]types.SecurityMaster, error) {
	return append([]types.SecurityMaster(nil), p.securities...), nil
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

func (p *Provider) Bars(tsCode types.SecurityID, from, to types.TradeDate) ([]types.Bar, error) {
	src := p.bars[tsCode]
	out := make([]types.Bar, 0, len(src))
	for _, b := range src {
		if inRange(b.TradeDate, from, to) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (p *Provider) ST() ([]types.STRecord, error) {
	return append([]types.STRecord(nil), p.st...), nil
}

func (p *Provider) Industry() ([]types.IndustryRecord, error) {
	return append([]types.IndustryRecord(nil), p.industry...), nil
}

func (p *Provider) Financials(tsCode types.SecurityID) ([]types.FinancialRow, error) {
	return append([]types.FinancialRow(nil), p.financials[tsCode]...), nil
}

func (p *Provider) MarketValues(tsCode types.SecurityID, from, to types.TradeDate) ([]types.MarketValue, error) {
	src := p.mvs[tsCode]
	out := make([]types.MarketValue, 0, len(src))
	for _, m := range src {
		if inRange(m.TradeDate, from, to) {
			out = append(out, m)
		}
	}
	return out, nil
}

func (p *Provider) IndexBars(indexCode string, from, to types.TradeDate) ([]types.IndexBar, error) {
	src := p.index[indexCode]
	out := make([]types.IndexBar, 0, len(src))
	for _, b := range src {
		if inRange(b.TradeDate, from, to) {
			out = append(out, b)
		}
	}
	return out, nil
}

// PublicTexts returns texts for tsCode with asof_ts ≤ end of asofEnd (Asia/Shanghai).
// Callers may further filter; this applies the PIT upper bound.
func (p *Provider) PublicTexts(tsCode types.SecurityID, asofEnd types.TradeDate) ([]types.PublicText, error) {
	end := asofEnd.Time().Add(24*time.Hour - time.Nanosecond) // end of day T
	out := make([]types.PublicText, 0)
	for _, t := range p.texts {
		if tsCode != "" && t.TSCode != tsCode {
			continue
		}
		if t.AsofTS.After(end) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// AllBars returns every loaded bar (test helper).
func (p *Provider) AllBars() ([]types.Bar, error) {
	var out []types.Bar
	for _, bars := range p.bars {
		out = append(out, bars...)
	}
	return out, nil
}

// BarOn returns the bar for tsCode on date T, or false if missing.
func (p *Provider) BarOn(tsCode types.SecurityID, t types.TradeDate) (types.Bar, bool) {
	for _, b := range p.bars[tsCode] {
		if b.TradeDate.Equal(t) {
			return b, true
		}
	}
	return types.Bar{}, false
}
