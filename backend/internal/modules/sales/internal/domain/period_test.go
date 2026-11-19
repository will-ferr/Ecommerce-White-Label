package domain

import (
	"testing"
	"time"
)

func TestParsePeriod(t *testing.T) {
	saoPaulo, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	// 01:30 UTC on Nov 20 is still Nov 19 in São Paulo.
	now := time.Date(2026, 11, 20, 1, 30, 0, 0, time.UTC)

	period, err := ParsePeriod("", "", saoPaulo, now)
	if err != nil {
		t.Fatal(err)
	}
	if period.From != "2026-10-21" || period.To != "2026-11-19" || len(period.Days()) != 30 {
		t.Errorf("default period = %+v (%d days)", period, len(period.Days()))
	}
	if !period.End.Equal(time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("end = %s, want midnight in São Paulo", period.End)
	}

	period, err = ParsePeriod("2026-11-01", "2026-11-01", saoPaulo, now)
	if err != nil || len(period.Days()) != 1 || period.End.Sub(period.Start) != 24*time.Hour {
		t.Errorf("single day = %+v, %v", period, err)
	}

	for name, dates := range map[string][2]string{
		"start after end": {"2026-11-10", "2026-11-01"},
		"bad start":       {"01/11/2026", ""},
		"bad end":         {"", "2026-13-01"},
		"too long":        {"2025-01-01", "2026-11-01"},
	} {
		if _, err := ParsePeriod(dates[0], dates[1], saoPaulo, now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParsePeriod("2025-11-20", "2026-11-19", saoPaulo, now); err != nil {
		t.Errorf("a full year was refused: %v", err)
	}
}
