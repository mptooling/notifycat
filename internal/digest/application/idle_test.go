package application

import (
	"testing"
	"time"
)

func TestReporter_IdleDays(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	cases := []struct {
		updated time.Time
		want    int
	}{
		{time.Date(2026, 6, 7, 23, 0, 0, 0, time.Local), 1},
		{time.Date(2026, 6, 5, 1, 0, 0, 0, time.Local), 3},
		{time.Date(2026, 6, 8, 0, 0, 0, 0, time.Local), 1}, // floored at 1
	}
	for _, c := range cases {
		if got := idleDays(now, c.updated); got != c.want {
			t.Errorf("idleDays(%v) = %d; want %d", c.updated, got, c.want)
		}
	}
}
