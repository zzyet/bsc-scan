package endpoint

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Manager implements services.Service and manages all RPC endpoints.
type Manager struct {
	db        *pgxpool.Pool
	endpoints map[int64]*Endpoint
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func NewManager(db *pgxpool.Pool) *Manager {
	return &Manager{
		db:        db,
		endpoints: make(map[int64]*Endpoint),
	}
}

func (m *Manager) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Initial sync from DB
	if err := m.syncFromDB(); err != nil {
		log.Printf("[endpoint-manager] Initial sync warning: %v", err)
	}

	// Periodic sync (every 5 minutes)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				if err := m.syncFromDB(); err != nil {
					log.Printf("[endpoint-manager] Sync error: %v", err)
				}
				m.saveState()
			}
		}
	}()

	log.Printf("[endpoint-manager] Started")
	return nil
}

func (m *Manager) Close() error {
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Lock()
	for _, ep := range m.endpoints {
		ep.Stop()
	}
	m.endpoints = make(map[int64]*Endpoint)
	m.mu.Unlock()
	m.wg.Wait()
	log.Printf("[endpoint-manager] Stopped")
	return nil
}

func (m *Manager) Name() string { return "EndpointManager" }
func (m *Manager) Ready() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.endpoints) == 0 {
		return fmt.Errorf("no endpoints configured")
	}
	return nil
}

func (m *Manager) HealthReport() map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	report := make(map[string]error)
	for id, ep := range m.endpoints {
		key := fmt.Sprintf("endpoint_%d", id)
		snap := ep.Snapshot()
		if snap.IsStopped {
			report[key] = fmt.Errorf("stopped")
		} else if snap.Status == StatusCircuitOpen {
			report[key] = fmt.Errorf("circuit_open")
		} else {
			report[key] = nil
		}
	}
	return report
}

// AcquireEndpoint returns a lease from any healthy endpoint.
func (m *Manager) AcquireEndpoint(ctx context.Context) (*Lease, error) {
	for {
		m.mu.RLock()
		var candidates []*Endpoint
		for _, ep := range m.endpoints {
			snap := ep.Snapshot()
			if !snap.IsStopped && snap.Status != StatusCircuitOpen && snap.Status != StatusStopped {
				candidates = append(candidates, ep)
			}
		}
		m.mu.RUnlock()

		if len(candidates) == 0 {
			select {
			case <-ctx.Done():
				return nil, ErrTokenTimeout
			case <-time.After(time.Second):
				continue
			}
		}

		// Round-robin: just try the first one
		for _, ep := range candidates {
			lease, err := ep.Acquire(ctx)
			if err == nil {
				return lease, nil
			}
			if err == ErrCircuitOpen || err == ErrEndpointStopped {
				continue
			}
		}
	}
}

func (m *Manager) syncFromDB() error {
	rows, err := m.db.Query(m.ctx,
		`SELECT id, url, rate_limit_per_minute, daily_limit, max_consecutive_failures,
		        max_total_failures, backoff_initial, backoff_max, daily_reset_hour, is_stopped
		 FROM endpoints`)
	if err != nil {
		return fmt.Errorf("query endpoints: %w", err)
	}
	defer rows.Close()

	dbEndpoints := make(map[int64]Config)
	for rows.Next() {
		var id int64
		var cfg Config
		var backoffInitialSec, backoffMaxSec int
		var isStopped bool
		if err := rows.Scan(&id, &cfg.URL, &cfg.RateLimitPerMinute, &cfg.DailyLimit,
			&cfg.MaxConsecutiveFailures, &cfg.MaxTotalFailures,
			&backoffInitialSec, &backoffMaxSec, &cfg.DailyResetHour, &isStopped); err != nil {
			return fmt.Errorf("scan endpoint: %w", err)
		}
		cfg.BackoffInitial = time.Duration(backoffInitialSec) * time.Second
		cfg.BackoffMax = time.Duration(backoffMaxSec) * time.Second
		dbEndpoints[id] = cfg
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove endpoints not in DB
	for id, ep := range m.endpoints {
		if _, ok := dbEndpoints[id]; !ok {
			ep.Stop()
			delete(m.endpoints, id)
			log.Printf("[endpoint-manager] Removed endpoint %d", id)
		}
	}

	// Add/update endpoints from DB
	for id, cfg := range dbEndpoints {
		if existing, ok := m.endpoints[id]; ok {
			// Update config if changed
			existing.Config = cfg
		} else {
			ep := NewBuilder().
				WithURL(cfg.URL).
				WithRateLimit(cfg.RateLimitPerMinute).
				WithDailyLimit(cfg.DailyLimit).
				WithMaxConsecutiveFailures(cfg.MaxConsecutiveFailures).
				WithMaxTotalFailures(cfg.MaxTotalFailures).
				WithBackoff(cfg.BackoffInitial, cfg.BackoffMax).
				WithDailyResetHour(cfg.DailyResetHour).
				Build()
			ep.ID = id
			m.endpoints[id] = ep
			m.wg.Add(1)
			go func(e *Endpoint) {
				defer m.wg.Done()
				e.Start(m.ctx)
			}(ep)
			log.Printf("[endpoint-manager] Added endpoint %d: %s", id, cfg.URL)
		}
	}

	return nil
}

func (m *Manager) saveState() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for id, ep := range m.endpoints {
		snap := ep.Snapshot()
		_, err := m.db.Exec(m.ctx,
			`UPDATE endpoints SET daily_used=$1, consecutive_failures=$2, total_failures=$3, status=$4, updated_at=NOW()
			 WHERE id=$5`,
			snap.DailyUsed, snap.ConsecutiveFailures, snap.TotalFailures, string(snap.Status), id)
		if err != nil {
			log.Printf("[endpoint-manager] Failed to save state for endpoint %d: %v", id, err)
		}
	}
}
