package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type blockRow struct {
	Number      int64      `json:"number"`
	Hash        string     `json:"hash"`
	ParentHash  *string    `json:"parentHash"`
	Timestamp   int64      `json:"timestamp"`
	Miner       *string    `json:"miner"`
	GasUsed     *int64     `json:"gasUsed"`
	GasLimit    *int64     `json:"gasLimit"`
	TxCount     int        `json:"txCount"`
	Status      string     `json:"status"`
	ProcessedAt *time.Time `json:"processedAt"`
	CreatedAt   *time.Time `json:"createdAt"`
}

func (h *Handler) ListBlocks(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := (page - 1) * limit

	var rows pgx.Rows
	var err error

	if status != "" {
		rows, err = h.db.Query(r.Context(),
			`SELECT block_number, hash, parent_hash, timestamp, miner, gas_used, gas_limit,
			        tx_count, status, processed_at, created_at
			 FROM blocks WHERE status=$1 ORDER BY block_number DESC LIMIT $2 OFFSET $3`,
			status, limit, offset)
	} else {
		rows, err = h.db.Query(r.Context(),
			`SELECT block_number, hash, parent_hash, timestamp, miner, gas_used, gas_limit,
			        tx_count, status, processed_at, created_at
			 FROM blocks ORDER BY block_number DESC LIMIT $1 OFFSET $2`,
			limit, offset)
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var total int64
	if status != "" {
		h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM blocks WHERE status=$1", status).Scan(&total)
	} else {
		h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM blocks").Scan(&total)
	}

	var blocks []blockRow
	for rows.Next() {
		var b blockRow
		if err := rows.Scan(&b.Number, &b.Hash, &b.ParentHash, &b.Timestamp,
			&b.Miner, &b.GasUsed, &b.GasLimit, &b.TxCount, &b.Status,
			&b.ProcessedAt, &b.CreatedAt); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		blocks = append(blocks, b)
	}

	writeJSON(w, 200, map[string]any{
		"data":  blocks,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *Handler) GetBlock(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.ParseInt(r.PathValue("number"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid block number")
		return
	}

	var b blockRow
	err = h.db.QueryRow(r.Context(),
		`SELECT block_number, hash, parent_hash, timestamp, miner, gas_used, gas_limit,
		        tx_count, status, processed_at, created_at
		 FROM blocks WHERE block_number=$1`, number,
	).Scan(&b.Number, &b.Hash, &b.ParentHash, &b.Timestamp,
		&b.Miner, &b.GasUsed, &b.GasLimit, &b.TxCount, &b.Status,
		&b.ProcessedAt, &b.CreatedAt)
	if err != nil {
		writeError(w, 404, "not found")
		return
	}
	writeJSON(w, 200, b)
}
