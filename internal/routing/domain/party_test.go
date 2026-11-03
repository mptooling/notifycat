package domain

import (
	"strconv"
	"strings"
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
	const formatRule = "use hours and minutes, like 15m, 2h or 1h30m"
	const rangeRule = "must be from " + PartyDeadlineRangeText
	testCases := []struct {
		name     string
		value    string
		wantRule string
	}{
		{name: "below minimum", value: "14m", wantRule: rangeRule},
		{name: "above maximum hours", value: "25h", wantRule: rangeRule},
		{name: "above maximum by a minute", value: "24h1m", wantRule: rangeRule},
		{name: "seconds", value: "30s", wantRule: formatRule},
		{name: "sixty seconds", value: "60s", wantRule: formatRule},
		{name: "trailing zero seconds", value: "1h30m0s", wantRule: formatRule},
		{name: "fraction", value: "1.5h", wantRule: formatRule},
		{name: "no unit", value: "90", wantRule: formatRule},
		{name: "negative", value: "-1h", wantRule: formatRule},
		{name: "empty", value: "", wantRule: formatRule},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParsePartyDeadline(testCase.value)

			require.Error(t, err)
			assert.ErrorContains(t, err, "party deadline "+strconv.Quote(testCase.value))
			assert.ErrorContains(t, err, testCase.wantRule)
		})
	}
}

func TestPartyDeadlineRangeText_MatchesTheBounds(t *testing.T) {
	lowestText, highestText, found := strings.Cut(PartyDeadlineRangeText, " to ")
	require.True(t, found)

	lowest, lowestErr := ParsePartyDeadline(lowestText)
	highest, highestErr := ParsePartyDeadline(highestText)

	require.NoError(t, lowestErr)
	require.NoError(t, highestErr)
	assert.Equal(t, MinPartyDeadline, lowest)
	assert.Equal(t, MaxPartyDeadline, highest)
}

func TestValidatePartyReviewers(t *testing.T) {
	testCases := []struct {
		name  string
		value int
	}{
		{name: "the minimum", value: MinPartyReviewers},
		{name: "five", value: 5},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidatePartyReviewers(testCase.value)

			require.NoError(t, err)
		})
	}
}

func TestValidatePartyReviewers_Rejects(t *testing.T) {
	testCases := []struct {
		name      string
		value     int
		wantValue string
	}{
		{name: "zero", value: 0, wantValue: "party reviewers 0"},
		{name: "negative", value: -1, wantValue: "party reviewers -1"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidatePartyReviewers(testCase.value)

			require.Error(t, err)
			assert.ErrorContains(t, err, testCase.wantValue)
			assert.ErrorContains(t, err, "must be 1 or more")
		})
	}
}
