package siprealtime

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
)

// ErrNotConfigured means REALTIME_CONFIG_JSON is unset.
var ErrNotConfigured = errors.New("siprealtime: REALTIME_CONFIG_JSON not set")

// Config holds realtime agent credentials and session options.
type Config struct {
	Credential       map[string]any
	SystemPrompt     string
	Voice            string
	InputSampleRate  int
	OutputSampleRate int
}

// ConfigFromEnv loads realtime settings from environment variables.
func ConfigFromEnv() (Config, error) {
	raw := strings.TrimSpace(os.Getenv("REALTIME_CONFIG_JSON"))
	if raw == "" {
		return Config{}, ErrNotConfigured
	}
	cfg := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return Config{}, err
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		if existing, ok := cfg["api_key"].(string); !ok || strings.TrimSpace(existing) == "" {
			cfg["api_key"] = v
		}
	}
	prompt := strings.TrimSpace(os.Getenv("REALTIME_SYSTEM_PROMPT"))
	if prompt == "" {
		prompt = "You are a helpful phone assistant. Reply concisely in the same language the caller speaks."
	}
	voice := strings.TrimSpace(os.Getenv("REALTIME_VOICE"))
	if voice == "" {
		voice = "Cherry"
	}
	return Config{
		Credential:       cfg,
		SystemPrompt:     prompt,
		Voice:            voice,
		InputSampleRate:  envInt("REALTIME_INPUT_SR", 16000),
		OutputSampleRate: envInt("REALTIME_OUTPUT_SR", 24000),
	}, nil
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
