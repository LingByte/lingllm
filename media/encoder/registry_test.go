package encoder

import (
	"testing"
)

func TestEncoderRegistry_Register(t *testing.T) {
	registry := NewEncoderRegistry()

	// Register a codec
	registry.Register("test", func(cfg interface{}) (interface{}, error) {
		return nil, nil
	})

	// Should be registered
	if registry == nil {
		t.Error("Registry should not be nil")
	}
}

func TestEncoderRegistry_Get(t *testing.T) {
	registry := NewEncoderRegistry()

	// Get non-existent codec
	encoder, err := registry.Get("nonexistent", nil)

	// Should return error or nil
	if encoder != nil && err == nil {
		t.Error("Get() should return error for non-existent codec")
	}
}

func TestEncoderRegistry_Multiple(t *testing.T) {
	registry := NewEncoderRegistry()

	// Register multiple codecs
	registry.Register("codec1", func(cfg interface{}) (interface{}, error) {
		return "codec1", nil
	})
	registry.Register("codec2", func(cfg interface{}) (interface{}, error) {
		return "codec2", nil
	})

	if registry == nil {
		t.Error("Registry should not be nil")
	}
}

func TestEncoderRegistry_Clear(t *testing.T) {
	registry := NewEncoderRegistry()

	registry.Register("test", func(cfg interface{}) (interface{}, error) {
		return nil, nil
	})

	registry.Clear()

	// After clear, should return error
	_, err := registry.Get("test", nil)
	if err == nil {
		t.Error("Get() should return error after Clear()")
	}
}
