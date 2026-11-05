package app

import (
	"testing"
	"time"
)

func TestNextRetryDelay(t *testing.T) {
	tests := []struct {
		current time.Duration
		want    time.Duration
	}{
		{0, time.Minute},
		{time.Minute, 2 * time.Minute},
		{2 * time.Minute, 4 * time.Minute},
		{16 * time.Minute, 30 * time.Minute},
		{30 * time.Minute, 30 * time.Minute},
		{time.Hour, 30 * time.Minute},
	}
	for _, tt := range tests {
		if got := nextRetryDelay(tt.current); got != tt.want {
			t.Errorf("nextRetryDelay(%s) = %s, want %s", tt.current, got, tt.want)
		}
	}
}
