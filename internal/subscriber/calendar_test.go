package subscriber

import (
	"testing"
	"time"
)

func TestMonthlyAnchorClampsWithoutDrift(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Aden")
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range []int{29, 30, 31} {
		p := Period{Kind: "monthly", Zone: "Asia/Aden", Anchor: time.Date(2024, 1, day, 0, 25, 0, 0, loc)}
		for _, c := range []struct {
			now                  string
			startDay, endDay     int
			startMonth, endMonth time.Month
		}{{"2024-02-29T00:25:00+03:00", 29, day, time.February, time.March}, {"2025-02-28T00:25:00+03:00", 28, day, time.February, time.March}, {"2025-03-01T00:00:00+03:00", 28, day, time.February, time.March}, {"2025-04-30T12:00:00+03:00", min(day, 30), day, time.April, time.May}} {
			now, err := time.Parse(time.RFC3339, c.now)
			if err != nil {
				t.Fatal(err)
			}
			start, end, err := p.Bounds(now)
			if err != nil || start.Day() != c.startDay || end.Day() != c.endDay || start.Month() != c.startMonth || end.Month() != c.endMonth || start.After(now) || !end.After(now) {
				t.Fatal(day, c, start, end, err)
			}
		}
	}
}
