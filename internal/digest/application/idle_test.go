package application

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestReporter_IdleDays(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	testCases := []struct {
		name    string
		updated time.Time
		want    int
	}{
		{"yesterday evening", time.Date(2026, 6, 7, 23, 0, 0, 0, time.Local), 1},
		{"three days back", time.Date(2026, 6, 5, 1, 0, 0, 0, time.Local), 3},
		{"same day floors at one", time.Date(2026, 6, 8, 0, 0, 0, 0, time.Local), 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, idleDays(now, testCase.updated))
		})
	}
}
