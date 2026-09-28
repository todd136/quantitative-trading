package fixture_test

import (
	"path/filepath"
	"testing"

	"quantitative-trading/internal/data/fixture"
	"quantitative-trading/internal/types"
)

func TestLoadFixtures(t *testing.T) {
	p, err := fixture.New(filepath.Join("..", "..", "..", "testdata", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	cal, err := p.LoadCalendar()
	if err != nil || len(cal) < 250 {
		t.Fatalf("calendar len=%d err=%v", len(cal), err)
	}
	secs, _ := p.Securities()
	boards := map[types.Board]bool{}
	for _, s := range secs {
		boards[s.Board] = true
	}
	for _, want := range []types.Board{types.BoardSSEMain, types.BoardSZSEMain, types.BoardChiNext, types.BoardSTAR, types.BoardBSE} {
		if !boards[want] {
			t.Fatalf("missing board %s in fixtures", want)
		}
	}
}
