package factors_test

import (
	"strings"
	"testing"

	"quantitative-trading/internal/factors"
	"quantitative-trading/internal/types"
)

func TestGetLatestFinancialRejectsZeroAnnouncement(t *testing.T) {
	tSignal := types.NewTradeDate(2024, 6, 1)
	rows := []types.FinancialRow{
		{
			TSCode: "600000.SH", ReportPeriod: types.NewTradeDate(2023, 12, 31),
			NetProfit: 1, Revenue: 1, Equity: 1, TotalAssets: 1,
		},
	}
	_, ok := factors.GetLatestFinancial(rows, tSignal)
	if ok {
		t.Fatal("zero announcement_date must be invisible")
	}
}

func TestStrictPITRejectsAnnounceEqualsPeriod(t *testing.T) {
	period := types.NewTradeDate(2023, 12, 31)
	realAnn := types.NewTradeDate(2024, 3, 30)
	tSignal := types.NewTradeDate(2024, 6, 1)
	proxy := types.FinancialRow{
		TSCode: "600000.SH", ReportPeriod: period, AnnouncementDate: period,
		AnnouncementIsProxy: true, NetProfit: 9, Revenue: 1, Equity: 1, TotalAssets: 1,
	}
	real := types.FinancialRow{
		TSCode: "600000.SH", ReportPeriod: period, AnnouncementDate: realAnn,
		NetProfit: 1, Revenue: 1, Equity: 1, TotalAssets: 1,
	}
	if _, ok := factors.GetLatestFinancialPIT([]types.FinancialRow{proxy}, tSignal, true); ok {
		t.Fatal("strict mode must reject announce≈period")
	}
	got, ok := factors.GetLatestFinancialPIT([]types.FinancialRow{proxy}, tSignal, false)
	if !ok || !got.AnnouncementDate.Equal(period) {
		t.Fatal("non-strict may still see proxy rows")
	}
	got, ok = factors.GetLatestFinancialPIT([]types.FinancialRow{proxy, real}, tSignal, true)
	if !ok || !got.AnnouncementDate.Equal(realAnn) {
		t.Fatalf("strict should pick NOTICE_DATE row, got %+v ok=%v", got, ok)
	}
	err := factors.AnnouncementUsable(proxy, tSignal, true)
	if err == nil || !strings.Contains(err.Error(), "announcement_date≈report_period") {
		t.Fatalf("usable err=%v", err)
	}
}

func TestStrictPITFutureAnnouncementStillInvisible(t *testing.T) {
	tSignal := types.NewTradeDate(2024, 2, 1)
	row := types.FinancialRow{
		TSCode:           "600000.SH",
		ReportPeriod:     types.NewTradeDate(2023, 12, 31),
		AnnouncementDate: types.NewTradeDate(2024, 3, 30),
		NetProfit:        1, Revenue: 1, Equity: 1, TotalAssets: 1,
	}
	if _, ok := factors.GetLatestFinancialPIT([]types.FinancialRow{row}, tSignal, true); ok {
		t.Fatal("announcement after signal must stay invisible")
	}
}

func TestMarketValueAsOfSkipsSnapshot(t *testing.T) {
	asof := types.NewTradeDate(2024, 1, 10)
	rows := []types.MarketValue{
		{TradeDate: types.NewTradeDate(2024, 1, 9), TotalMV: 100, IsSnapshot: true, TSCode: "600000.SH"},
		{TradeDate: types.NewTradeDate(2024, 1, 8), TotalMV: 80, TSCode: "600000.SH"},
	}
	mv, ok := factors.MarketValueAsOf(rows, asof, 10)
	if !ok || mv != 80 {
		t.Fatalf("got %v ok=%v want 80 (skip snapshot)", mv, ok)
	}
	_, err := factors.MarketValueAsOfPIT([]types.MarketValue{rows[0]}, asof, 10, true)
	if err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("strict snapshot-only err=%v", err)
	}
}

func TestValidateSTAndIndustryStrict(t *testing.T) {
	st := []types.STRecord{{
		TSCode: "600002.SH", EntryDate: factors.STSnapshotEpoch, PITBackfill: true, STType: "ST",
	}}
	if err := factors.ValidateSTForStrict(st); err == nil {
		t.Fatal("want ST backfill error")
	}
	inds := []types.IndustryRecord{{
		TSCode: "600000.SH", IndustryCode: "银行", EffectiveDate: factors.IndustryBackfillEpoch,
		Source: "EM_INDUSTRY", PITBackfill: true,
	}}
	if err := factors.ValidateIndustryForStrict(inds); err == nil {
		t.Fatal("want industry backfill error")
	}
	okInd := []types.IndustryRecord{{
		TSCode: "600000.SH", IndustryCode: "801780", EffectiveDate: types.NewTradeDate(2020, 1, 1),
		Source: "SW_L1",
	}}
	if err := factors.ValidateIndustryForStrict(okInd); err != nil {
		t.Fatal(err)
	}
}
