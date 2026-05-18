package endpoint

import (
	"context"
	"sync"
	"time"
)

// Status represents the endpoint health status.
type Status string

const (
	StatusUnknown    Status = "unknown"
	StatusHealthy    Status = "healthy"
	StatusUnhealthy  Status = "unhealthy"
	StatusCircuitOpen Status = "circuit_open"
	StatusHalfOpen   Status = "half_open"
	StatusStopped    Status = "stopped"
)

// Config defines an endpoint's static configuration.
type Config struct {
	URL                   string
	RateLimitPerMinute    int
	DailyLimit            int
	MaxConsecutiveFailures int
	MaxTotalFailures      int
	BackoffInitial        time.Duration
	BackoffMax            time.Duration
	DailyResetHour        int
}

// Endpoint represents a single RPC endpoint with runtime state.
type Endpoint struct {
	ID      int64
	Config  Config
	Status  Status

	tokenBucket *TokenBucket
	breaker     *CircuitBreaker

	// Runtime counters (saved to DB periodically)
	dailyUsed          int
	consecutiveFailures int
	totalFailures      int
	lastResetTime      time.Time
	isStopped          bool

	mu sync.RWMutex
}

// Lease is returned to consumers when they acquire a token.
type Lease struct {
	endpoint *Endpoint
	success  bool
	reported bool
	mu       sync.Mutex
}

// ReportSuccess marks the request as successful.
func (l *Lease) ReportSuccess() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reported {
		return
	}
	l.reported = true
	l.success = true
	l.endpoint.reportResult(true)
}

// ReportFailure marks the request as failed.
func (l *Lease) ReportFailure() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reported {
		return
	}
	l.reported = true
	l.endpoint.reportResult(false)
}

// AutoReport ensures the lease is reported when the context is cancelled (timeout).
func (l *Lease) AutoReport() {
	if !l.reported {
		l.reported = true
	}
}

// Config returns the endpoint's configuration.
func (l *Lease) Config() Config {
	return l.endpoint.Config
}

func (ep *Endpoint) reportResult(success bool) {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	if success {
		ep.consecutiveFailures = 0
		ep.dailyUsed++
	} else {
		ep.consecutiveFailures++
		ep.totalFailures++
	}

	// Check permanent stop
	if ep.Config.MaxTotalFailures > 0 && ep.totalFailures >= ep.Config.MaxTotalFailures {
		ep.isStopped = true
		ep.Status = StatusStopped
		ep.tokenBucket.Stop()
		return
	}

	// Check circuit breaker
	if ep.Config.MaxConsecutiveFailures > 0 && ep.consecutiveFailures >= ep.Config.MaxConsecutiveFailures {
		ep.Status = StatusCircuitOpen
		ep.tokenBucket.Stop()
		ep.breaker.Trip()
	}
}

// StatusSnapshot returns a copy of the current runtime state.
type StatusSnapshot struct {
	ID                  int64
	Status              Status
	DailyUsed           int
	ConsecutiveFailures int
	TotalFailures       int
	IsStopped           bool
}

func (ep *Endpoint) Snapshot() StatusSnapshot {
	ep.mu.RLock()
	defer ep.mu.RUnlock()
	return StatusSnapshot{
		ID:                  ep.ID,
		Status:              ep.Status,
		DailyUsed:           ep.dailyUsed,
		ConsecutiveFailures: ep.consecutiveFailures,
		TotalFailures:       ep.totalFailures,
		IsStopped:           ep.isStopped,
	}
}

func (ep *Endpoint) ResetDailyIfNeeded(now time.Time) {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	resetHour := ep.Config.DailyResetHour
	resetTime := time.Date(now.Year(), now.Month(), now.Day(), resetHour, 0, 0, 0, now.Location())
	if now.After(resetTime) && ep.lastResetTime.Before(resetTime) {
		ep.dailyUsed = 0
		ep.lastResetTime = now
	}
}

// NewBuilder creates a Builder for Config.
type Builder struct {
	cfg Config
}

func NewBuilder() *Builder {
	return &Builder{
		cfg: Config{
			RateLimitPerMinute:    60,
			MaxConsecutiveFailures: 5,
			BackoffInitial:        time.Minute,
			BackoffMax:            10 * time.Minute,
		},
	}
}

func (b *Builder) WithURL(url string) *Builder          { b.cfg.URL = url; return b }
func (b *Builder) WithRateLimit(rpm int) *Builder       { b.cfg.RateLimitPerMinute = rpm; return b }
func (b *Builder) WithDailyLimit(limit int) *Builder     { b.cfg.DailyLimit = limit; return b }
func (b *Builder) WithMaxConsecutiveFailures(n int) *Builder { b.cfg.MaxConsecutiveFailures = n; return b }
func (b *Builder) WithMaxTotalFailures(n int) *Builder    { b.cfg.MaxTotalFailures = n; return b }
func (b *Builder) WithBackoff(initial, max time.Duration) *Builder {
	b.cfg.BackoffInitial = initial
	b.cfg.BackoffMax = max
	return b
}
func (b *Builder) WithDailyResetHour(hour int) *Builder { b.cfg.DailyResetHour = hour; return b }

func (b *Builder) Build() *Endpoint {
	cfg := b.cfg
	ep := &Endpoint{
		Config:        cfg,
		Status:        StatusUnknown,
		lastResetTime: time.Now(),
	}
	ep.tokenBucket = NewTokenBucket(cfg.RateLimitPerMinute, cfg.DailyLimit)
	ep.breaker = NewCircuitBreaker(cfg.BackoffInitial, cfg.BackoffMax, func() {
		// Half-open probe: allow token bucket to generate one token for testing
		ep.mu.Lock()
		ep.Status = StatusHalfOpen
		ep.tokenBucket.ResetForProbe()
		ep.mu.Unlock()
	})
	return ep
}

// Acquire blocks until a token is available, returning a Lease.
func (ep *Endpoint) Acquire(ctx context.Context) (*Lease, error) {
	ep.mu.RLock()
	stopped := ep.isStopped
	status := ep.Status
	ep.mu.RUnlock()

	if stopped {
		return nil, ErrEndpointStopped
	}
	if status == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	token, err := ep.tokenBucket.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	_ = token // token consumed
	return &Lease{endpoint: ep}, nil
}

// Start begins the endpoint's goroutines.
func (ep *Endpoint) Start(ctx context.Context) {
	ep.mu.Lock()
	ep.Status = StatusHealthy
	ep.mu.Unlock()
	ep.tokenBucket.Start(ctx)
}

// Stop shuts down the endpoint.
func (ep *Endpoint) Stop() {
	ep.tokenBucket.Stop()
}
