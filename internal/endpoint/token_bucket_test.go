package endpoint

import (
	"context"
	"testing"
	"time"
)

func TestTokenBucket_Rate(t *testing.T) {
	// 30 tokens per minute = 1 token every 2 seconds
	tb := NewTokenBucket(30, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tb.Start(ctx)

	// Wait for initial pre-fill
	time.Sleep(100 * time.Millisecond)

	// Should be able to acquire at least 20 tokens quickly (pre-fill + early production)
	acquired := 0
	deadline := time.After(3 * time.Second) // Wait 3 seconds for additional production
loop:
	for acquired < 30 {
		select {
		case <-deadline:
			break loop
		default:
		}
		actx, acancel := context.WithTimeout(ctx, 500*time.Millisecond)
		_, err := tb.Acquire(actx)
		acancel()
		if err != nil {
			break loop
		}
		acquired++
	}

	if acquired < 25 {
		t.Errorf("Expected at least 25 tokens in 3s, got %d", acquired)
	}
	tb.Stop()
}

func TestTokenBucket_DailyLimit(t *testing.T) {
	tb := NewTokenBucket(10, 5) // 10/min but daily limit 5
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tb.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Acquire 5 tokens (Acquire increments dailyUsed internally)
	for i := 0; i < 5; i++ {
		actx, acancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := tb.Acquire(actx)
		acancel()
		if err != nil {
			t.Fatalf("Token %d: unexpected error: %v", i+1, err)
		}
	}

	// 6th token should fail with daily quota exceeded
	actx, acancel := context.WithTimeout(ctx, 1*time.Second)
	_, err := tb.Acquire(actx)
	acancel()
	if err != ErrDailyQuotaExceeded {
		t.Errorf("Expected ErrDailyQuotaExceeded after daily limit, got: %v", err)
	}
	tb.Stop()
}

func TestTokenBucket_Stop(t *testing.T) {
	tb := NewTokenBucket(60, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tb.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	tb.Stop()

	// Should not be able to acquire after stop
	actx, acancel := context.WithTimeout(ctx, 500*time.Millisecond)
	_, err := tb.Acquire(actx)
	acancel()
	if err != ErrEndpointStopped {
		t.Errorf("Expected ErrEndpointStopped after Stop(), got: %v", err)
	}
}

func TestTokenBucket_ResetForProbe(t *testing.T) {
	tb := NewTokenBucket(600, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tb.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Drain all tokens
	for i := 0; i < 600; i++ {
		select {
		case <-tb.tokens:
		default:
			goto drained
		}
	}
drained:

	// Reset for probe should add exactly one token
	tb.ResetForProbe()
	select {
	case <-tb.tokens:
		// OK, got one probe token
	default:
		t.Error("Expected one probe token after ResetForProbe()")
	}

	// Should be empty after consuming probe token
	select {
	case <-tb.tokens:
		t.Error("Expected only one probe token")
	default:
		// OK
	}
	tb.Stop()
}
