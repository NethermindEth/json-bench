package sanitize

import (
	"errors"
	"strings"
	"testing"
)

func TestLogValue(t *testing.T) {
	t.Run("strips CR LF NUL TAB", func(t *testing.T) {
		got := LogValue("hello\r\nworld\x00x\ty")
		want := "helloworldxy"
		if got != want {
			t.Errorf("LogValue = %q, want %q", got, want)
		}
	})

	t.Run("truncates long input", func(t *testing.T) {
		input := strings.Repeat("a", maxLogValueLen+50)
		got := LogValue(input)
		if len(got) != maxLogValueLen+3 {
			t.Errorf("unexpected length: %d", len(got))
		}
		if !strings.HasSuffix(got, "...") {
			t.Errorf("expected truncation suffix, got %q", got[len(got)-5:])
		}
	})

	t.Run("trims surrounding whitespace", func(t *testing.T) {
		got := LogValue("  padded  ")
		if got != "padded" {
			t.Errorf("LogValue = %q, want %q", got, "padded")
		}
	})

	t.Run("passes safe input through", func(t *testing.T) {
		got := LogValue("legit-id_v2.0")
		if got != "legit-id_v2.0" {
			t.Errorf("LogValue = %q, want passthrough", got)
		}
	})
}

func TestLogError(t *testing.T) {
	t.Run("scrubs CR LF from error message", func(t *testing.T) {
		got := LogError(errors.New("baseline not found: ../bad\r\nINJECTED"))
		want := "baseline not found: ../badINJECTED"
		if got.Error() != want {
			t.Errorf("LogError = %q, want %q", got.Error(), want)
		}
	})

	t.Run("returns nil for nil input", func(t *testing.T) {
		if LogError(nil) != nil {
			t.Errorf("LogError(nil) should be nil")
		}
	})
}
