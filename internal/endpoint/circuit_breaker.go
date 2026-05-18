package endpoint

import (
	"sync"
	"time"
)

// CircuitBreaker implements the circuit breaker pattern with exponential backoff.
type CircuitBreaker struct {
	backoffInitial time.Duration
	backoffMax     time.Duration
	currentBackoff time.Duration
	onHalfOpen     func() // Called when entering half-open state
	timer          *time.Timer
	mu             sync.Mutex
	active         bool
}

func NewCircuitBreaker(initial, max time.Duration, onHalfOpen func()) *CircuitBreaker {
	return &CircuitBreaker{
		backoffInitial: initial,
		backoffMax:     max,
		currentBackoff: initial,
		onHalfOpen:     onHalfOpen,
	}
}

// Trip opens the circuit breaker and schedules a half-open probe.
func (cb *CircuitBreaker) Trip() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.active {
		return
	}
	cb.active = true

	backoff := cb.currentBackoff
	cb.timer = time.AfterFunc(backoff, func() {
		cb.mu.Lock()
		cb.onHalfOpen()
		cb.mu.Unlock()
	})
}

// Reset restores the breaker to its initial state (on successful recovery).
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.active = false
	cb.currentBackoff = cb.backoffInitial
	if cb.timer != nil {
		cb.timer.Stop()
	}
}

// IncreaseBackoff doubles the backoff (on repeated failure).
func (cb *CircuitBreaker) IncreaseBackoff() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.currentBackoff *= 2
	if cb.currentBackoff > cb.backoffMax {
		cb.currentBackoff = cb.backoffMax
	}
	cb.active = false
}

// IsOpen returns true if the circuit is currently open.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.active
}
