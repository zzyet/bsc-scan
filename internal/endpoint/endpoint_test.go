package endpoint

import (
	"context"
	"testing"
	"time"
)

func TestEndpoint_AcquireAndReportSuccess(t *testing.T) {
	ep := NewBuilder().
		WithURL("http://localhost:8545").
		WithRateLimit(600).
		Build()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ep.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	lease, err := ep.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	lease.ReportSuccess()

	snap := ep.Snapshot()
	if snap.DailyUsed != 1 {
		t.Errorf("Expected DailyUsed=1, got %d", snap.DailyUsed)
	}
	if snap.Status != StatusHealthy {
		t.Errorf("Expected Healthy status, got %s", snap.Status)
	}

	ep.Stop()
}

func TestEndpoint_CircuitOpenAfterConsecutiveFailures(t *testing.T) {
	ep := NewBuilder().
		WithURL("http://localhost:8545").
		WithRateLimit(600).
		WithMaxConsecutiveFailures(3).
		Build()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ep.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Fail 3 times consecutively
	for i := 0; i < 3; i++ {
		lease, err := ep.Acquire(ctx)
		if err != nil {
			t.Fatalf("Acquire %d failed: %v", i+1, err)
		}
		lease.ReportFailure()
	}

	snap := ep.Snapshot()
	if snap.Status != StatusCircuitOpen {
		t.Errorf("Expected CircuitOpen after %d failures, got %s", 3, snap.Status)
	}
	if snap.ConsecutiveFailures != 3 {
		t.Errorf("Expected ConsecutiveFailures=3, got %d", snap.ConsecutiveFailures)
	}
}

func TestEndpoint_PermanentStop(t *testing.T) {
	ep := NewBuilder().
		WithURL("http://localhost:8545").
		WithRateLimit(600).
		WithMaxTotalFailures(2).
		Build()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ep.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Fail 2 times total
	for i := 0; i < 2; i++ {
		lease, err := ep.Acquire(ctx)
		if err != nil {
			t.Fatalf("Acquire %d failed: %v", i+1, err)
		}
		lease.ReportFailure()
	}

	snap := ep.Snapshot()
	if !snap.IsStopped {
		t.Error("Expected endpoint to be permanently stopped")
	}
	if snap.Status != StatusStopped {
		t.Errorf("Expected Stopped status, got %s", snap.Status)
	}

	// Subsequent Acquire should fail
	_, err := ep.Acquire(ctx)
	if err != ErrEndpointStopped {
		t.Errorf("Expected ErrEndpointStopped, got %v", err)
	}
}

func TestEndpoint_DailyReset(t *testing.T) {
	ep := NewBuilder().
		WithURL("http://localhost:8545").
		WithRateLimit(600).
		WithDailyLimit(1000).
		WithDailyResetHour(0). // midnight
		Build()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ep.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	lease, _ := ep.Acquire(ctx)
	lease.ReportSuccess()

	snap := ep.Snapshot()
	if snap.DailyUsed != 1 {
		t.Errorf("Expected DailyUsed=1, got %d", snap.DailyUsed)
	}

	// Reset at same day shouldn't reset
	ep.ResetDailyIfNeeded(time.Now())
	snap = ep.Snapshot()
	if snap.DailyUsed != 1 {
		t.Errorf("DailyUsed should still be 1 on same day, got %d", snap.DailyUsed)
	}

	ep.Stop()
}

func TestEndpoint_LeaseDoubleReport(t *testing.T) {
	ep := NewBuilder().
		WithURL("http://localhost:8545").
		WithRateLimit(600).
		Build()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ep.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	lease, _ := ep.Acquire(ctx)
	lease.ReportSuccess()
	lease.ReportFailure() // Should be no-op (already reported)

	snap := ep.Snapshot()
	if snap.DailyUsed != 1 {
		t.Errorf("Expected DailyUsed=1 (success counted), got %d", snap.DailyUsed)
	}
	if snap.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures should be 0, got %d", snap.ConsecutiveFailures)
	}

	ep.Stop()
}
