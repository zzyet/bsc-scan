package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bsc-scan/admin/handler"
	"bsc-scan/internal/config"
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

	h := handler.New(db)

	mux := http.NewServeMux()

	// Serve frontend static files
	fs := http.FileServer(http.Dir("frontend"))
	mux.Handle("GET /", fs)

	// Endpoints CRUD
	mux.HandleFunc("GET /api/endpoints", h.ListEndpoints)
	mux.HandleFunc("POST /api/endpoints", h.CreateEndpoint)
	mux.HandleFunc("GET /api/endpoints/{id}", h.GetEndpoint)
	mux.HandleFunc("PUT /api/endpoints/{id}", h.UpdateEndpoint)
	mux.HandleFunc("DELETE /api/endpoints/{id}", h.DeleteEndpoint)
	mux.HandleFunc("POST /api/endpoints/{id}/stop", h.StopEndpoint)

	// Blocks
	mux.HandleFunc("GET /api/blocks", h.ListBlocks)
	mux.HandleFunc("GET /api/blocks/{number}", h.GetBlock)

	// Contracts
	mux.HandleFunc("GET /api/contracts", h.ListContracts)
	mux.HandleFunc("POST /api/contracts", h.CreateContract)
	mux.HandleFunc("PUT /api/contracts/{address}", h.UpdateContract)
	mux.HandleFunc("DELETE /api/contracts/{address}", h.DeleteContract)
	mux.HandleFunc("GET /api/contracts/{address}/transactions", h.ListContractTransactions)

	// Transactions
	mux.HandleFunc("GET /api/transactions/{hash}", h.GetTransaction)

	// Stats
	mux.HandleFunc("GET /api/stats", h.GetStats)

	server := &http.Server{
		Addr:         ":8080",
		Handler:      corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Printf("[admin] Listening on :8080")
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("[admin] Server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Printf("[admin] Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	server.Shutdown(shutdownCtx)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
