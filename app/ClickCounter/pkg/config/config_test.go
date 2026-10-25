package config

import (
	"testing"
	"time"
)

func TestGetEnvDurationSlice(t *testing.T) {
	fallback := []time.Duration{time.Minute, 2 * time.Minute}

	t.Run("valid list", func(t *testing.T) {
		t.Setenv("FLUSH_RETRY_DELAYS", "1m, 2m,4m,8m")
		got := getEnvDurationSlice("FLUSH_RETRY_DELAYS", fallback)
		want := []time.Duration{
			1 * time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute,
		}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("got[%d] = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("unset uses fallback", func(t *testing.T) {
		t.Setenv("FLUSH_RETRY_DELAYS", "")
		got := getEnvDurationSlice("FLUSH_RETRY_DELAYS", fallback)
		if len(got) != len(fallback) || got[0] != fallback[0] {
			t.Errorf("got %v, want %v", got, fallback)
		}
	})

	t.Run("invalid element falls back entirely", func(t *testing.T) {
		t.Setenv("FLUSH_RETRY_DELAYS", "1m,banana")
		got := getEnvDurationSlice("FLUSH_RETRY_DELAYS", fallback)
		if len(got) != len(fallback) {
			t.Errorf("got %v, want fallback %v", got, fallback)
		}
	})

	t.Run("non-positive duration falls back", func(t *testing.T) {
		t.Setenv("FLUSH_RETRY_DELAYS", "1m,0s")
		got := getEnvDurationSlice("FLUSH_RETRY_DELAYS", fallback)
		if len(got) != len(fallback) {
			t.Errorf("got %v, want fallback %v", got, fallback)
		}
	})
}
