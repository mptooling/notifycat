package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePartyDeadline(t *testing.T) {
	testCases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "minimum", value: "15m", want: 15 * time.Minute},
		{name: "hours", value: "2h", want: 2 * time.Hour},
		{name: "hours and minutes", value: "1h30m", want: 90 * time.Minute},
		{name: "maximum", value: "24h", want: 24 * time.Hour},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := ParsePartyDeadline(testCase.value)

			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestParsePartyDeadline_Rejects(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{name: "below minimum", value: "14m"},
		{name: "above maximum hours", value: "25h"},
		{name: "above maximum by a minute", value: "24h1m"},
		{name: "seconds", value: "30s"},
		{name: "sixty seconds", value: "60s"},
		{name: "trailing zero seconds", value: "1h30m0s"},
		{name: "fraction", value: "1.5h"},
		{name: "no unit", value: "90"},
		{name: "negative", value: "-1h"},
		{name: "empty", value: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParsePartyDeadline(testCase.value)

			assert.Error(t, err)
		})
	}
}

func TestValidatePartyReviewers(t *testing.T) {
	testCases := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{name: "one", value: 1},
		{name: "five", value: 5},
		{name: "zero", value: 0, wantErr: true},
		{name: "negative", value: -1, wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidatePartyReviewers(testCase.value)

			assert.Equal(t, testCase.wantErr, err != nil)
		})
	}
}
