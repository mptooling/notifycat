package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNotificationsConfig_Specs(t *testing.T) {
	testCases := []struct {
		name          string
		notifications NotificationsConfig
		want          []string
	}{
		{
			name:          "zero value is per-event",
			notifications: NotificationsConfig{},
			want:          nil,
		},
		{
			name:          "per-event has no schedule",
			notifications: NotificationsConfig{Strategy: StrategyPerEvent},
			want:          nil,
		},
		{
			name:          "hourly fires at the top of every hour",
			notifications: NotificationsConfig{Strategy: StrategyScheduled, Mode: ScheduleModeHourly},
			want:          []string{"0 * * * *"},
		},
		{
			name: "at fires once a day per time",
			notifications: NotificationsConfig{
				Strategy: StrategyScheduled,
				Mode:     ScheduleModeAt,
				At:       []TimeOfDay{{Hour: 10, Minute: 30}, {Hour: 15, Minute: 0}},
			},
			want: []string{"30 10 * * *", "0 15 * * *"},
		},
		{
			name: "cron is used as written",
			notifications: NotificationsConfig{
				Strategy: StrategyScheduled,
				Mode:     ScheduleModeCron,
				Cron:     []string{"0 9 * * 1-5", "0 14 * * 1-5"},
			},
			want: []string{"0 9 * * 1-5", "0 14 * * 1-5"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			specs := testCase.notifications.Specs()

			assert.Equal(t, testCase.want, specs)
		})
	}
}

func TestNotificationsConfig_Scheduled(t *testing.T) {
	assert.False(t, NotificationsConfig{}.Scheduled(), "an unset strategy is per-event")
	assert.False(t, NotificationsConfig{Strategy: StrategyPerEvent}.Scheduled())
	assert.True(t, NotificationsConfig{Strategy: StrategyScheduled, Mode: ScheduleModeHourly}.Scheduled())
}
