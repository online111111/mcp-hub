package manager

import (
	"time"
)

// Option configures Manager behavior.
type Option func(*Manager)

// WithSessionFactory injects a custom SessionFactory (e.g. for testing with fake sessions).
func WithSessionFactory(f SessionFactory) Option {
	return func(m *Manager) {
		if f != nil {
			m.factory = f
		}
	}
}

// WithBackoffDelays customizes the retry backoff duration sequence.
func WithBackoffDelays(delays []time.Duration) Option {
	return func(m *Manager) {
		if len(delays) > 0 {
			m.backoffDelays = delays
		}
	}
}

// WithReadyResetDuration sets the continuous Ready duration before resetting failure counts.
func WithReadyResetDuration(d time.Duration) Option {
	return func(m *Manager) {
		if d > 0 {
			m.readyResetDur = d
		}
	}
}

// WithDrainTimeout sets the timeout for draining in-flight leases during shutdown or reload.
func WithDrainTimeout(d time.Duration) Option {
	return func(m *Manager) {
		if d > 0 {
			m.drainTimeout = d
		}
	}
}

// WithDialConcurrency sets the maximum concurrent startup dials (defaults to 4).
func WithDialConcurrency(n int) Option {
	return func(m *Manager) {
		if n > 0 {
			m.limiter = make(chan struct{}, n)
		}
	}
}
