package handler

import (
	"net/http"
)

func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats := make(map[string]any)

	var blockCount int64
	h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM blocks").Scan(&blockCount)
	stats["totalBlocks"] = blockCount

	h.db.QueryRow(r.Context(),
		"SELECT COUNT(*) FROM blocks WHERE status='processed'").Scan(&blockCount)
	stats["processedBlocks"] = blockCount

	h.db.QueryRow(r.Context(),
		"SELECT COUNT(*) FROM blocks WHERE status='unprocessed'").Scan(&blockCount)
	stats["unprocessedBlocks"] = blockCount

	var txCount int64
	h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM transactions").Scan(&txCount)
	stats["totalTransactions"] = txCount

	var contractTxCount int64
	h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM contract_transactions").Scan(&contractTxCount)
	stats["contractTransactions"] = contractTxCount

	var endpointCount int64
	h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM endpoints").Scan(&endpointCount)
	stats["endpoints"] = endpointCount

	h.db.QueryRow(r.Context(),
		"SELECT COUNT(*) FROM endpoints WHERE is_stopped=false").Scan(&endpointCount)
	stats["activeEndpoints"] = endpointCount

	var contractCount int64
	h.db.QueryRow(r.Context(),
		"SELECT COUNT(*) FROM monitored_contracts WHERE active=true").Scan(&contractCount)
	stats["monitoredContracts"] = contractCount

	// Latest block
	var latestBlock *int64
	h.db.QueryRow(r.Context(),
		"SELECT MAX(block_number) FROM blocks WHERE status='processed'").Scan(&latestBlock)
	if latestBlock == nil {
		stats["latestBlock"] = 0
	} else {
		stats["latestBlock"] = *latestBlock
	}

	writeJSON(w, 200, stats)
}
