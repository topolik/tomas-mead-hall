package config

import "testing"

func TestEffectiveLookbackHours(t *testing.T) {
	cases := []struct {
		name      string
		lookback  int
		interval  int
		want      int
	}{
		{"default when unset", 0, 5, 72},          // 3 days, the configured default
		{"explicit value honored", 120, 5, 120},   // 5 days
		{"floored at 2x interval", 1, 2400, 80},    // 2400m*2=4800m → ceil 80h > 1h configured
		{"configured above floor wins", 72, 30, 72},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ScheduleConfig{LookbackHours: c.lookback}.EffectiveLookbackHours(c.interval)
			if got != c.want {
				t.Errorf("EffectiveLookbackHours(lookback=%d, interval=%d) = %d, want %d", c.lookback, c.interval, got, c.want)
			}
		})
	}
}
