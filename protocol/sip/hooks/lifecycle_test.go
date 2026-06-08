package hooks

import "testing"

func TestNopLifecycle(t *testing.T) {
	var s LifecycleSink = NopLifecycle{}
	s.OnEstablished(CallMeta{CallID: "c1"})
	s.OnBye(CallMeta{}, HangupMeta{}, RecordingArtifact{})
}

func TestNopRecording(t *testing.T) {
	var r RecordingSink = NopRecording{}
	if r.BeginRecording(CallMeta{CallID: "c1"}, 8000, "pcmu") {
		t.Fatal("nop should disable")
	}
	if _, err := r.FinishRecording(nil, RecordingArtifact{}); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultRegistry(t *testing.T) {
	if DefaultRegistry.Lifecycle == nil || DefaultRegistry.Recording == nil {
		t.Fatal("default registry")
	}
}
