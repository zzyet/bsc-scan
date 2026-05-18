package endpoint

import (
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_TripAndRecover(t *testing.T) {
	var halfOpenCalled bool
	var mu sync.Mutex

	cb := NewCircuitBreaker(50*time.Millisecond, 200*time.Millisecond, func() {
		mu.Lock()
		halfOpenCalled = true
		mu.Unlock()
	})

	// Initial state: not open
	if cb.IsOpen() {
		t.Error("Circuit should not be open initially")
	}

	// Trip
	cb.Trip()
	if !cb.IsOpen() {
		t.Error("Circuit should be open after Trip()")
	}

	// Wait for half-open probe
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	called := halfOpenCalled
	mu.Unlock()
	if !called {
		t.Error("onHalfOpen should have been called")
	}

	// Reset
	cb.Reset()
	if cb.IsOpen() {
		t.Error("Circuit should be closed after Reset()")
	}
}

func TestCircuitBreaker_ExponentialBackoff(t *testing.T) {
	backoffCalls := 0
	cb := NewCircuitBreaker(10*time.Millisecond, 100*time.Millisecond, func() {
		backoffCalls++
	})

	// First trip — backoff = 10ms
	cb.Trip()
	time.Sleep(30 * time.Millisecond)

	// Increase backoff (simulate half-open failure)
	cb.IncreaseBackoff()
	// Now backoff should be 20ms

	// Second trip
	cb.Trip()
	time.Sleep(50 * time.Millisecond)

	// Increase again
	cb.IncreaseBackoff()
	// Now backoff should be 40ms

	// Third trip
	cb.Trip()
	time.Sleep(60 * time.Millisecond)

	if backoffCalls < 3 {
		t.Errorf("Expected at least 3 half-open callbacks, got %d", backoffCalls)
	}

	// Backoff should cap at max
	for i := 0; i < 10; i++ {
		cb.IncreaseBackoff()
	}
	// After many increases, backoff should be capped at max (100ms)
	cb.Trip()
	time.Sleep(120 * time.Millisecond)
	if backoffCalls < 4 {
		t.Errorf("Expected half-open callback even after max backoff, got %d", backoffCalls)
	}
}

func TestCircuitBreaker_DoubleTrip(t *testing.T) {
	cb := NewCircuitBreaker(50*time.Millisecond, 200*time.Millisecond, func() {})

	cb.Trip()
	if !cb.IsOpen() {
		t.Error("Circuit should be open after first Trip()")
	}

	// Second Trip() should be a no-op
	cb.Trip()
	if !cb.IsOpen() {
		t.Error("Circuit should still be open after second Trip()")
	}
}

func TestCircuitBreaker_ResetStopsTimer(t *testing.T) {
	halfOpenCalled := false
	cb := NewCircuitBreaker(50*time.Millisecond, 200*time.Millisecond, func() {
		halfOpenCalled = true
	})

	cb.Trip()
	time.Sleep(10 * time.Millisecond)

	// Reset before timer fires
	cb.Reset()

	// Wait past the original timer
	time.Sleep(60 * time.Millisecond)

	if halfOpenCalled {
		t.Error("halfOpen should NOT have been called — Reset() should cancel timer")
	}
}
