package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"bsc-scan/internal/config"

	"github.com/smartcontractkit/chainlink-common/pkg/services"
)

// Orchestrator manages service lifecycle.
type Orchestrator struct {
	services []services.Service
	cfg      *config.Config
}

func New() *Orchestrator {
	return &Orchestrator{}
}

func (o *Orchestrator) SetConfig(cfg *config.Config) {
	o.cfg = cfg
}

func (o *Orchestrator) Register(svc services.Service) {
	o.services = append(o.services, svc)
}

func (o *Orchestrator) Start(ctx context.Context) error {
	for _, svc := range o.services {
		log.Printf("[orch] Starting service...")
		if err := svc.Start(ctx); err != nil {
			return fmt.Errorf("failed to start service: %w", err)
		}
	}
	log.Printf("[orch] All services started")
	return nil
}

func (o *Orchestrator) Close() error {
	log.Printf("[orch] Shutting down...")
	for i := len(o.services) - 1; i >= 0; i-- {
		if err := o.services[i].Close(); err != nil {
			log.Printf("[orch] Error closing service: %v", err)
		}
	}
	log.Printf("[orch] All services stopped")
	return nil
}

func (o *Orchestrator) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if err := o.Start(ctx); err != nil {
		return err
	}

	select {
	case sig := <-sigCh:
		log.Printf("[orch] Received signal: %v", sig)
	case <-ctx.Done():
	}

	return o.Close()
}
