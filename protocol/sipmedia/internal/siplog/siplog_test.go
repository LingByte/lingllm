package siplog

import "testing"

func TestStandardLogger(t *testing.T) {
	if L == nil {
		t.Fatal("nil logger")
	}
}
