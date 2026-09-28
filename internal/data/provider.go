// Package data defines the market-data Provider interface and shared helpers.
package data

import (
	"quantitative-trading/internal/types"
)

// Provider loads PIT-safe market / fundamental / text data for the backtest.
type Provider interface {
	LoadCalendar() ([]types.TradeDate, error)
	Securities() ([]types.SecurityMaster, error)
	Bars(tsCode types.SecurityID, from, to types.TradeDate) ([]types.Bar, error)
	ST() ([]types.STRecord, error)
	Industry() ([]types.IndustryRecord, error)
	Financials(tsCode types.SecurityID) ([]types.FinancialRow, error)
	MarketValues(tsCode types.SecurityID, from, to types.TradeDate) ([]types.MarketValue, error)
	IndexBars(indexCode string, from, to types.TradeDate) ([]types.IndexBar, error)
	PublicTexts(tsCode types.SecurityID, asofEnd types.TradeDate) ([]types.PublicText, error)
}

// AllBars is a convenience for providers that can dump full bar sets (tests/fixtures).
type AllBarsProvider interface {
	Provider
	AllBars() ([]types.Bar, error)
}
