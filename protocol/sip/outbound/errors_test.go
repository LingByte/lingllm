package outbound

import (
	"errors"
	"testing"
)

func TestErrors_SentinelValues(t *testing.T) {
	if !errors.Is(ErrNoSignalingSender, ErrNoSignalingSender) {
		t.Fatal("ErrNoSignalingSender")
	}
	if ErrNoSignalingSender.Error() == "" {
		t.Fatal("message required")
	}
	if !errors.Is(ErrNotImplemented, ErrNotImplemented) {
		t.Fatal("ErrNotImplemented")
	}
}
