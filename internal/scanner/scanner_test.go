package scanner

import (
	"testing"
	"time"

	"bsc-scan/internal/config"
)

func TestNew(t *testing.T) {
	cfg := config.ScannerConfig{
		StartBlock:   48000000,
		WorkerCount:  5,
		BatchSize:    50,
		PollInterval: 3 * time.Second,
	}

	s := New(nil, nil, nil, cfg)
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.cfg.StartBlock != 48000000 {
		t.Errorf("Expected StartBlock=48000000, got %d", s.cfg.StartBlock)
	}
	if s.cfg.WorkerCount != 5 {
		t.Errorf("Expected WorkerCount=5, got %d", s.cfg.WorkerCount)
	}
	if s.cfg.BatchSize != 50 {
		t.Errorf("Expected BatchSize=50, got %d", s.cfg.BatchSize)
	}
}

func TestScanner_SetMonitor(t *testing.T) {
	s := New(nil, nil, nil, config.ScannerConfig{})
	if s.monitor != nil {
		t.Error("monitor should be nil initially")
	}

	// SetMonitor with nil — should work
	s.SetMonitor(nil)
	if s.monitor != nil {
		t.Error("monitor should still be nil after SetMonitor(nil)")
	}
}

func TestScanner_ServiceInterface(t *testing.T) {
	s := New(nil, nil, nil, config.ScannerConfig{
		WorkerCount: 2,
		BatchSize:   10,
	})

	if s.Name() != "BlockScanner" {
		t.Errorf("Expected Name()='BlockScanner', got '%s'", s.Name())
	}
	if err := s.Ready(); err != nil {
		t.Errorf("Ready() should return nil, got %v", err)
	}
	report := s.HealthReport()
	if report["scanner"] != nil {
		t.Errorf("HealthReport should report scanner=nil, got %v", report["scanner"])
	}
}

func TestScanner_ConfigDefaults(t *testing.T) {
	cfg := config.ScannerConfig{
		WorkerCount:  0,
		BatchSize:    0,
		PollInterval: 0,
	}

	s := New(nil, nil, nil, cfg)
	if s.cfg.WorkerCount != 0 {
		t.Errorf("WorkerCount should be 0, got %d", s.cfg.WorkerCount)
	}
}
