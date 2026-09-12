package segment

import (
	"testing"
	"time"
)

func TestAdjustStep(t *testing.T) {
	cases := []struct {
		name     string
		cur      int64
		interval time.Duration
		want     int64
	}{
		{"间隔极短视为极快消耗翻倍", 1000, 0, 2000},
		{"消耗快于15min翻倍", 1000, 10 * time.Minute, 2000},
		{"翻倍封顶50000", 40000, time.Minute, 50000},
		{"已到上限不再翻倍", 50000, time.Minute, 50000},
		{"消耗慢于30min减半", 1000, time.Hour, 500},
		{"减半下限100", 150, time.Hour, 100},
		{"已到下限不再减半", 100, time.Hour, 100},
		{"中等消耗保持不变", 1000, 20 * time.Minute, 1000},
	}
	for _, c := range cases {
		if got := adjustStep(c.cur, c.interval); got != c.want {
			t.Errorf("%s: adjustStep(%d, %v) = %d, want %d", c.name, c.cur, c.interval, got, c.want)
		}
	}
}
