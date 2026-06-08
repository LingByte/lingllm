package siprealtime

import (
	"errors"
	"os"
	"testing"
)

func TestConfigFromEnv_NotConfigured(t *testing.T) {
	os.Unsetenv("REALTIME_CONFIG_JSON")
	_, err := ConfigFromEnv()
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v", err)
	}
}

func TestConfigFromEnv_OK(t *testing.T) {
	t.Setenv("REALTIME_CONFIG_JSON", `{"provider":"aliyun_omni","api_key":"k"}`)
	t.Setenv("REALTIME_VOICE", "Ethan")
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Voice != "Ethan" || cfg.InputSampleRate != 16000 {
		t.Fatalf("cfg: %+v", cfg)
	}
}
