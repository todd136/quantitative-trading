// Package akshare is a thin adapter stub around an external AKShare data boundary.
//
// Real network fetches are optional and skipped in unit tests. The default test path
// uses internal/data/fixture. This package must compile without a live Python env.
//
// Boundary options (pick one at integration time):
//  1. CLI: invoke `python scripts/akshare_fetch.py --date YYYY-MM-DD --out /tmp/...`
//  2. HTTP: call a local sidecar that wraps AKShare REST endpoints
//
// Neither path is exercised by `go test ./...` when provider=fixture.
package akshare

import (
	"fmt"
	"os/exec"

	"quantitative-trading/internal/data"
	"quantitative-trading/internal/types"
)

// Config controls how the adapter reaches AKShare.
type Config struct {
	PythonBin  string // default "python3"
	HelperPath string // path to scripts/akshare_fetch.py
	HTTPBase   string // optional sidecar base URL; if set, preferred over CLI
	SkipNetwork bool  // when true, all Load* return ErrSkipped
}

// ErrSkipped is returned when network/CLI access is disabled (tests / offline).
var ErrSkipped = fmt.Errorf("akshare: network/CLI skipped (use fixture provider in tests)")

// Provider is a compile-time stub implementing data.Provider.
// Methods either skip or shell out to the documented helper; they do not embed scraping logic.
type Provider struct {
	cfg Config
}

var _ data.Provider = (*Provider)(nil)

// New constructs an AKShare adapter. Pass SkipNetwork=true for compile/smoke tests.
func New(cfg Config) *Provider {
	if cfg.PythonBin == "" {
		cfg.PythonBin = "python3"
	}
	return &Provider{cfg: cfg}
}

func (p *Provider) skip() error {
	if p.cfg.SkipNetwork {
		return ErrSkipped
	}
	return nil
}

// FetchViaCLI documents / optionally invokes the Python helper. Returns combined output.
// Unit tests should not call this with SkipNetwork=false without a mock helper.
func (p *Provider) FetchViaCLI(args ...string) ([]byte, error) {
	if err := p.skip(); err != nil {
		return nil, err
	}
	if p.cfg.HelperPath == "" {
		return nil, fmt.Errorf("akshare: HelperPath not configured")
	}
	cmdArgs := append([]string{p.cfg.HelperPath}, args...)
	cmd := exec.Command(p.cfg.PythonBin, cmdArgs...)
	return cmd.CombinedOutput()
}

func (p *Provider) LoadCalendar() ([]types.TradeDate, error) {
	if err := p.skip(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("akshare: LoadCalendar not wired — implement via CLI/HTTP sidecar")
}

func (p *Provider) Securities() ([]types.SecurityMaster, error) {
	if err := p.skip(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("akshare: Securities not wired")
}

func (p *Provider) Bars(tsCode types.SecurityID, from, to types.TradeDate) ([]types.Bar, error) {
	if err := p.skip(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("akshare: Bars(%s) not wired", tsCode)
}

func (p *Provider) ST() ([]types.STRecord, error) {
	if err := p.skip(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("akshare: ST not wired")
}

func (p *Provider) Industry() ([]types.IndustryRecord, error) {
	if err := p.skip(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("akshare: Industry not wired")
}

func (p *Provider) Financials(tsCode types.SecurityID) ([]types.FinancialRow, error) {
	if err := p.skip(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("akshare: Financials not wired")
}

func (p *Provider) MarketValues(tsCode types.SecurityID, from, to types.TradeDate) ([]types.MarketValue, error) {
	if err := p.skip(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("akshare: MarketValues not wired")
}

func (p *Provider) IndexBars(indexCode string, from, to types.TradeDate) ([]types.IndexBar, error) {
	if err := p.skip(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("akshare: IndexBars not wired")
}

func (p *Provider) PublicTexts(tsCode types.SecurityID, asofEnd types.TradeDate) ([]types.PublicText, error) {
	if err := p.skip(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("akshare: PublicTexts not wired")
}
