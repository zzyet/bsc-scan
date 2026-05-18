package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

type ContractRequest struct {
	Name   string `json:"name"`
	Active *bool  `json:"active"`
}

type contractRow struct {
	Address   string     `json:"address"`
	Name      *string    `json:"name"`
	Active    bool       `json:"active"`
	CreatedAt *time.Time `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

func (h *Handler) ListContracts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT address, name, active, created_at, updated_at
		 FROM monitored_contracts ORDER BY created_at DESC`)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var contracts []contractRow
	for rows.Next() {
		var c contractRow
		if err := rows.Scan(&c.Address, &c.Name, &c.Active, &c.CreatedAt, &c.UpdatedAt); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		contracts = append(contracts, c)
	}
	writeJSON(w, 200, contracts)
}

func (h *Handler) CreateContract(w http.ResponseWriter, r *http.Request) {
	var req ContractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}

	active := true
	if req.Active != nil {
		active = *req.Active
	}

	address := r.URL.Query().Get("address")
	if address == "" {
		writeError(w, 400, "address required")
		return
	}

	_, err := h.db.Exec(r.Context(),
		`INSERT INTO monitored_contracts (address, name, active)
		 VALUES ($1,$2,$3) ON CONFLICT (address) DO UPDATE SET name=$2, active=$3, updated_at=NOW()`,
		address, req.Name, active)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]string{"address": address, "status": "created"})
}

func (h *Handler) UpdateContract(w http.ResponseWriter, r *http.Request) {
	address := r.PathValue("address")
	var req ContractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}

	if req.Name != "" && req.Active != nil {
		_, err := h.db.Exec(r.Context(),
			`UPDATE monitored_contracts SET name=$1, active=$2, updated_at=NOW() WHERE address=$3`,
			req.Name, *req.Active, address)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
	} else if req.Name != "" {
		_, err := h.db.Exec(r.Context(),
			`UPDATE monitored_contracts SET name=$1, updated_at=NOW() WHERE address=$2`,
			req.Name, address)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
	} else if req.Active != nil {
		_, err := h.db.Exec(r.Context(),
			`UPDATE monitored_contracts SET active=$1, updated_at=NOW() WHERE address=$2`,
			*req.Active, address)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
	}

	writeJSON(w, 200, map[string]string{"status": "updated"})
}

func (h *Handler) DeleteContract(w http.ResponseWriter, r *http.Request) {
	address := r.PathValue("address")
	_, err := h.db.Exec(r.Context(), "DELETE FROM monitored_contracts WHERE address=$1", address)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

func (h *Handler) ListContractTransactions(w http.ResponseWriter, r *http.Request) {
	address := r.PathValue("address")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := (page - 1) * limit

	rows, err := h.db.Query(r.Context(),
		`SELECT tx_hash, contract_address, block_number, block_timestamp,
		        from_addr, to_addr, value, gas_used, gas_price, status,
		        method_selector, input_data, created_at
		 FROM contract_transactions
		 WHERE contract_address=$1
		 ORDER BY block_number DESC LIMIT $2 OFFSET $3`,
		address, limit, offset)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	type ContractTx struct {
		TxHash          string     `json:"txHash"`
		ContractAddress string     `json:"contractAddress"`
		BlockNumber     int64      `json:"blockNumber"`
		BlockTimestamp  *int64     `json:"blockTimestamp"`
		FromAddr        string     `json:"fromAddr"`
		ToAddr          string     `json:"toAddr"`
		Value           string     `json:"value"`
		GasUsed         int64      `json:"gasUsed"`
		GasPrice        string     `json:"gasPrice"`
		Status          int16      `json:"status"`
		MethodSelector  string     `json:"methodSelector"`
		InputData       string     `json:"inputData"`
		CreatedAt       *time.Time `json:"createdAt"`
	}

	var txs []ContractTx
	for rows.Next() {
		var tx ContractTx
		if err := rows.Scan(&tx.TxHash, &tx.ContractAddress, &tx.BlockNumber,
			&tx.BlockTimestamp, &tx.FromAddr, &tx.ToAddr, &tx.Value,
			&tx.GasUsed, &tx.GasPrice, &tx.Status, &tx.MethodSelector,
			&tx.InputData, &tx.CreatedAt); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		txs = append(txs, tx)
	}

	var total int64
	h.db.QueryRow(r.Context(),
		"SELECT COUNT(*) FROM contract_transactions WHERE contract_address=$1", address,
	).Scan(&total)

	writeJSON(w, 200, map[string]any{
		"data":  txs,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *Handler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")

	type txRow struct {
		Hash        string     `json:"hash"`
		BlockNumber int64      `json:"blockNumber"`
		FromAddr    *string    `json:"fromAddr"`
		ToAddr      *string    `json:"toAddr"`
		Value       *string    `json:"value"`
		Gas         *int64     `json:"gas"`
		GasPrice    *string    `json:"gasPrice"`
		InputData   *string    `json:"inputData"`
		Status      *int16     `json:"status"`
		CreatedAt   *time.Time `json:"createdAt"`
	}

	var tx txRow
	err := h.db.QueryRow(r.Context(),
		`SELECT tx_hash, block_number, from_addr, to_addr, value, gas, gas_price,
		        input_data, status, created_at
		 FROM transactions WHERE tx_hash=$1`, hash,
	).Scan(&tx.Hash, &tx.BlockNumber, &tx.FromAddr, &tx.ToAddr, &tx.Value,
		&tx.Gas, &tx.GasPrice, &tx.InputData, &tx.Status, &tx.CreatedAt)
	if err != nil {
		writeError(w, 404, "not found")
		return
	}

	logRows, _ := h.db.Query(r.Context(),
		`SELECT log_index, address, topic_0, topic_1, topic_2, topic_3, data
		 FROM event_logs WHERE tx_hash=$1 ORDER BY log_index`, hash)
	var logs []map[string]any
	if logRows != nil {
		defer logRows.Close()
		for logRows.Next() {
			var logIndex int
			var addr, t0, t1, t2, t3, data *string
			logRows.Scan(&logIndex, &addr, &t0, &t1, &t2, &t3, &data)
			logs = append(logs, map[string]any{
				"logIndex": logIndex,
				"address":  addr,
				"topic0":   t0,
				"topic1":   t1,
				"topic2":   t2,
				"topic3":   t3,
				"data":     data,
			})
		}
	}

	writeJSON(w, 200, map[string]any{
		"transaction": tx,
		"logs":        logs,
	})
}
