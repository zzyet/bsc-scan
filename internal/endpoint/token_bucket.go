package endpoint

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrEndpointStopped    = errors.New("endpoint is permanently stopped")
	ErrCircuitOpen        = errors.New("circuit breaker is open")
	ErrDailyQuotaExceeded = errors.New("daily quota exceeded")
	ErrTokenTimeout       = errors.New("timed out waiting for token")
)

// TokenBucket distributes tokens at a uniform rate.
type TokenBucket struct {
	tokens    chan struct{}
	ratePerMin int
	dailyLimit int
	dailyUsed  int
	mu         sync.Mutex
	stopCh     chan struct{}
	running    bool
}

func NewTokenBucket(ratePerMin, dailyLimit int) *TokenBucket {
	capacity := ratePerMin * 2
	if capacity < 10 {
		capacity = 10
	}
	return &TokenBucket{
		tokens:    make(chan struct{}, capacity),
		ratePerMin: ratePerMin,
		dailyLimit: dailyLimit,
		stopCh:    make(chan struct{}),
	}
}

// Start begins token production.
func (tb *TokenBucket) Start(ctx context.Context) {
	tb.mu.Lock()
	if tb.running {
		tb.mu.Unlock()
		return
	}
	tb.running = true
	tb.mu.Unlock()

	// Pre-fill with initial tokens
	for i := 0; i < tb.ratePerMin; i++ {
		select {
		case tb.tokens <- struct{}{}:
		default:
		}
	}

	interval := time.Minute / time.Duration(tb.ratePerMin)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-tb.stopCh:
			return
		case <-ticker.C:
			tb.mu.Lock()
			if tb.dailyLimit > 0 && tb.dailyUsed >= tb.dailyLimit {
				tb.mu.Unlock()
				continue
			}
			tb.mu.Unlock()

			select {
			case tb.tokens <- struct{}{}:
			default:
				// Bucket full, drop token
			}
		}
	}
}

// ResetForProbe generates one token for half-open circuit probing.
func (tb *TokenBucket) ResetForProbe() {
	// Drain existing tokens
	for {
		select {
		case <-tb.tokens:
		default:
			goto done
		}
	}
done:
	// Add one probe token
	tb.tokens <- struct{}{}
}

// Stop halts token production.
func (tb *TokenBucket) Stop() {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	if tb.running {
		tb.running = false
		close(tb.stopCh)
	}
}

// Acquire blocks until a token is available.
func (tb *TokenBucket) Acquire(ctx context.Context) (struct{}, error) {
	tb.mu.Lock()
	if !tb.running {
		tb.mu.Unlock()
		return struct{}{}, ErrEndpointStopped
	}
	if tb.dailyLimit > 0 && tb.dailyUsed >= tb.dailyLimit {
		tb.mu.Unlock()
		return struct{}{}, ErrDailyQuotaExceeded
	}
	tb.mu.Unlock()

	select {
	case token := <-tb.tokens:
		tb.mu.Lock()
		tb.dailyUsed++
		tb.mu.Unlock()
		return token, nil
	case <-ctx.Done():
		return struct{}{}, ErrTokenTimeout
	}
}
