package main

import (
	"context"
	"log"
	"time"

	"bsc-scan/internal/config"
	"bsc-scan/internal/endpoint"
	"bsc-scan/internal/monitor"
	"bsc-scan/internal/orchestrator"
	"bsc-scan/internal/scanner"
	"bsc-scan/internal/store"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := store.NewDB(ctx, cfg.Database.DSN, cfg.Database.MaxConnections)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	batchWriter := store.NewBatchWriter(db)

	epMgr := endpoint.NewManager(db)
	blkScanner := scanner.New(db, batchWriter, epMgr, cfg.Scanner)
	contractMonitor := monitor.New(db, batchWriter)

	orch := orchestrator.New()
	orch.Register(epMgr)
	orch.Register(blkScanner)
	orch.Register(contractMonitor)
	orch.SetConfig(cfg)

	if err := orch.Run(); err != nil {
		log.Fatalf("Orchestrator error: %v", err)
	}
}
