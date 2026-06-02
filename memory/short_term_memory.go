package memory

import (
	"sync"
	"time"
)

// ShortTermMemory represents short-term memory for recent interactions.
//
// L2: Short-Term Memory (短期记忆，最近的交互)
// Stores recent interactions with decay over time.
type ShortTermMemory struct {
	// Recent interactions
	interactions []Interaction

	// Configuration
	maxSize      int           // Max number of interactions (default 50)
	decayRate    float32       // How much to decay per hour (default 0.1)
	retentionTTL time.Duration // How long to keep interactions (default 24h)

	mu sync.RWMutex
}

// Interaction represents a single interaction in short-term memory.
type Interaction struct {
	ID        string
	Timestamp time.Time
	Type      InteractionType
	Content   string
	Metadata  map[string]interface{}
	Importance float32 // 0-1, higher = more important
	AccessCount int     // How many times accessed
}

// InteractionType represents the type of interaction.
type InteractionType string

const (
	InteractionTypeMessage      InteractionType = "message"
	InteractionTypeAction       InteractionType = "action"
	InteractionTypeObservation  InteractionType = "observation"
	InteractionTypeDecision     InteractionType = "decision"
	InteractionTypeError        InteractionType = "error"
)

// NewShortTermMemory creates a new short-term memory.
func NewShortTermMemory() *ShortTermMemory {
	return &ShortTermMemory{
		interactions: make([]Interaction, 0, 50),
		maxSize:      50,
		decayRate:    0.1,
		retentionTTL: 24 * time.Hour,
	}
}

// AddInteraction adds an interaction to short-term memory.
func (s *ShortTermMemory) AddInteraction(id string, iType InteractionType, content string, importance float32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	interaction := Interaction{
		ID:          id,
		Timestamp:   time.Now(),
		Type:        iType,
		Content:     content,
		Metadata:    make(map[string]interface{}),
		Importance:  importance,
		AccessCount: 0,
	}

	s.interactions = append(s.interactions, interaction)

	// Keep only maxSize interactions
	if len(s.interactions) > s.maxSize {
		s.interactions = s.interactions[1:]
	}
}

// GetRecentInteractions returns recent interactions with decay applied.
func (s *ShortTermMemory) GetRecentInteractions(count int) []Interaction {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if count > len(s.interactions) {
		count = len(s.interactions)
	}

	// Get the most recent interactions
	start := len(s.interactions) - count
	if start < 0 {
		start = 0
	}

	interactions := make([]Interaction, count)
	copy(interactions, s.interactions[start:])

	// Apply decay
	now := time.Now()
	for i := range interactions {
		age := now.Sub(interactions[i].Timestamp)
		decayFactor := 1.0 - (float32(age.Hours()) * s.decayRate)
		if decayFactor < 0 {
			decayFactor = 0
		}
		interactions[i].Importance *= decayFactor
	}

	return interactions
}

// GetInteractionsByType returns interactions of a specific type.
func (s *ShortTermMemory) GetInteractionsByType(iType InteractionType) []Interaction {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Interaction
	for _, interaction := range s.interactions {
		if interaction.Type == iType {
			result = append(result, interaction)
		}
	}
	return result
}

// AccessInteraction marks an interaction as accessed (increases importance).
func (s *ShortTermMemory) AccessInteraction(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.interactions {
		if s.interactions[i].ID == id {
			s.interactions[i].AccessCount++
			s.interactions[i].Importance += 0.05 // Boost importance on access
			if s.interactions[i].Importance > 1.0 {
				s.interactions[i].Importance = 1.0
			}
			break
		}
	}
}

// CleanupExpired removes expired interactions.
func (s *ShortTermMemory) CleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var cleaned []Interaction

	for _, interaction := range s.interactions {
		if now.Sub(interaction.Timestamp) < s.retentionTTL {
			cleaned = append(cleaned, interaction)
		}
	}

	s.interactions = cleaned
}

// GetStats returns statistics about short-term memory.
func (s *ShortTermMemory) GetStats() ShortTermMemoryStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := ShortTermMemoryStats{
		TotalInteractions: len(s.interactions),
		ByType:            make(map[InteractionType]int),
		AverageImportance: 0,
	}

	totalImportance := float32(0)
	for _, interaction := range s.interactions {
		stats.ByType[interaction.Type]++
		totalImportance += interaction.Importance
	}

	if len(s.interactions) > 0 {
		stats.AverageImportance = totalImportance / float32(len(s.interactions))
	}

	return stats
}

// ShortTermMemoryStats represents statistics about short-term memory.
type ShortTermMemoryStats struct {
	TotalInteractions int
	ByType            map[InteractionType]int
	AverageImportance float32
}

// SetDecayRate sets the decay rate.
func (s *ShortTermMemory) SetDecayRate(rate float32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rate >= 0 && rate <= 1 {
		s.decayRate = rate
	}
}

// SetMaxSize sets the maximum number of interactions.
func (s *ShortTermMemory) SetMaxSize(size int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if size > 0 {
		s.maxSize = size
	}
}

// Clear clears all interactions.
func (s *ShortTermMemory) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.interactions = make([]Interaction, 0, s.maxSize)
}

// GetAll returns all interactions.
func (s *ShortTermMemory) GetAll() []Interaction {
	s.mu.RLock()
	defer s.mu.RUnlock()

	interactions := make([]Interaction, len(s.interactions))
	copy(interactions, s.interactions)
	return interactions
}
