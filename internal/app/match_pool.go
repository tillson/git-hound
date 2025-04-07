package app

import (
	"sync"
)

// MatchPool implements an object pool for Match structs
type MatchPool struct {
	pool sync.Pool
}

// Global match pool singleton
var globalMatchPool = &MatchPool{
	pool: sync.Pool{
		New: func() interface{} {
			// Create a new Match with pre-allocated attributes slice
			return &Match{
				Attributes: make([]string, 0, 4), // Pre-allocate with capacity for typical use
			}
		},
	},
}

// GetMatch gets a Match from the pool or creates a new one if none are available
func GetMatch() *Match {
	return globalMatchPool.pool.Get().(*Match)
}

// PutMatch returns a Match to the pool for reuse
func PutMatch(m *Match) {
	// Reset the match to avoid leaking data
	m.Text = ""
	m.Attributes = m.Attributes[:0] // Keep capacity but reset length to 0
	m.Line = Line{}
	m.Commit = ""
	m.CommitFile = ""
	m.File = ""
	m.Expression = ""

	// Return to pool
	globalMatchPool.pool.Put(m)
}

// GetMatches gets multiple matches at once
func GetMatches(count int) []*Match {
	matches := make([]*Match, count)
	for i := 0; i < count; i++ {
		matches[i] = GetMatch()
	}
	return matches
}

// PutMatches returns multiple matches to the pool
func PutMatches(matches []*Match) {
	for _, m := range matches {
		if m != nil {
			PutMatch(m)
		}
	}
}
