package akshare_test

import (
	"errors"
	"testing"

	"quantitative-trading/internal/data/akshare"
	"quantitative-trading/internal/types"
)

func TestSkipNetwork(t *testing.T) {
	p := akshare.New(akshare.Config{SkipNetwork: true})
	_, err := p.LoadCalendar()
	if !errors.Is(err, akshare.ErrSkipped) {
		t.Fatalf("want ErrSkipped, got %v", err)
	}
	_, err = p.Bars("600000.SH", types.TradeDate{}, types.TradeDate{})
	if !errors.Is(err, akshare.ErrSkipped) {
		t.Fatalf("want ErrSkipped, got %v", err)
	}
}
