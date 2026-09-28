package backtest

import "quantitative-trading/internal/config"

// BuyCost returns commission+slippage cost for a buy notional (no stamp tax).
func BuyCost(cfg config.Config, notional float64) float64 {
	return notional * cfg.BuyCostRate()
}

// SellCost returns commission+stamp+slippage cost for a sell notional.
func SellCost(cfg config.Config, notional float64) float64 {
	return notional * cfg.SellCostRate()
}
