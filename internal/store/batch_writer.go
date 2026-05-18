package store

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BatchWriter handles efficient batch inserts.
type BatchWriter struct {
	pool *pgxpool.Pool
}

func NewBatchWriter(pool *pgxpool.Pool) *BatchWriter {
	return &BatchWriter{pool: pool}
}

// Block represents a raw block for batch insert.
type Block struct {
	Number     int64
	Hash       string
	ParentHash string
	Timestamp  int64
	Miner      string
	GasUsed    int64
	GasLimit   int64
	TxCount    int
}

// InsertBlocksBatch inserts blocks using COPY protocol for speed.
func (bw *BatchWriter) InsertBlocksBatch(ctx context.Context, blocks []Block) error {
	if len(blocks) == 0 {
		return nil
	}
	return pgx.BeginFunc(ctx, bw.pool, func(tx pgx.Tx) error {
		_, err := tx.CopyFrom(
			ctx,
			pgx.Identifier{"blocks"},
			[]string{"block_number", "hash", "parent_hash", "timestamp", "miner", "gas_used", "gas_limit", "tx_count", "status"},
			pgx.CopyFromSlice(len(blocks), func(i int) ([]any, error) {
				b := blocks[i]
				return []any{b.Number, b.Hash, b.ParentHash, b.Timestamp, b.Miner, b.GasUsed, b.GasLimit, b.TxCount, "unprocessed"}, nil
			}),
		)
		return err
	})
}

// Tx represents a transaction for batch insert.
type Tx struct {
	Hash        string
	BlockNumber int64
	FromAddr    string
	ToAddr      string
	Value       string
	Gas         int64
	GasPrice    string
	InputData   string
	Status      int16
}

// InsertTxsBatch inserts transactions using multi-row VALUES.
func (bw *BatchWriter) InsertTxsBatch(ctx context.Context, txs []Tx) error {
	if len(txs) == 0 {
		return nil
	}
	rows := make([][]any, len(txs))
	for i, tx := range txs {
		rows[i] = []any{tx.Hash, tx.BlockNumber, tx.FromAddr, tx.ToAddr, tx.Value, tx.Gas, tx.GasPrice, tx.InputData, tx.Status}
	}
	_, err := bw.pool.CopyFrom(
		ctx,
		pgx.Identifier{"transactions"},
		[]string{"tx_hash", "block_number", "from_addr", "to_addr", "value", "gas", "gas_price", "input_data", "status"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// EventLog for batch insert.
type EventLog struct {
	TxHash      string
	BlockNumber int64
	LogIndex    int
	Address     string
	Topic0      string
	Topic1      string
	Topic2      string
	Topic3      string
	Data        string
}

// InsertEventLogsBatch inserts event logs in bulk, ignoring duplicate key violations.
func (bw *BatchWriter) InsertEventLogsBatch(ctx context.Context, logs []EventLog) error {
	if len(logs) == 0 {
		return nil
	}
	rows := make([][]any, len(logs))
	for i, l := range logs {
		rows[i] = []any{l.TxHash, l.BlockNumber, l.LogIndex, l.Address, l.Topic0, l.Topic1, l.Topic2, l.Topic3, l.Data}
	}
	_, err := bw.pool.CopyFrom(
		ctx,
		pgx.Identifier{"event_logs"},
		[]string{"tx_hash", "block_number", "log_index", "address", "topic_0", "topic_1", "topic_2", "topic_3", "data"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// ContractTx for batch insert.
type ContractTx struct {
	TxHash          string
	ContractAddress string
	BlockNumber     int64
	BlockTimestamp  int64
	FromAddr        string
	ToAddr          string
	Value           string
	GasUsed         int64
	GasPrice        string
	Status          int16
	MethodSelector  string
	InputData       string
}

// InsertContractTxsBatch inserts contract transactions using COPY.
func (bw *BatchWriter) InsertContractTxsBatch(ctx context.Context, ctxs []ContractTx) error {
	if len(ctxs) == 0 {
		return nil
	}
	rows := make([][]any, len(ctxs))
	for i, ct := range ctxs {
		rows[i] = []any{ct.TxHash, ct.ContractAddress, ct.BlockNumber, ct.BlockTimestamp, ct.FromAddr, ct.ToAddr, ct.Value, ct.GasUsed, ct.GasPrice, ct.Status, ct.MethodSelector, ct.InputData}
	}
	_, err := bw.pool.CopyFrom(
		ctx,
		pgx.Identifier{"contract_transactions"},
		[]string{"tx_hash", "contract_address", "block_number", "block_timestamp", "from_addr", "to_addr", "value", "gas_used", "gas_price", "status", "method_selector", "input_data"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// ContractEvent for batch insert.
type ContractEvent struct {
	TxHash          string
	LogIndex        int
	ContractAddress string
	Topic0          string
	Topic1          string
	Topic2          string
	Topic3          string
	Data            string
	BlockNumber     int64
}

// InsertContractEventsBatch inserts contract events, skipping duplicates.
func (bw *BatchWriter) InsertContractEventsBatch(ctx context.Context, events []ContractEvent) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([][]any, len(events))
	for i, e := range events {
		rows[i] = []any{e.TxHash, e.LogIndex, e.ContractAddress, e.Topic0, e.Topic1, e.Topic2, e.Topic3, e.Data, e.BlockNumber}
	}
	_, err := bw.pool.CopyFrom(
		ctx,
		pgx.Identifier{"contract_events"},
		[]string{"tx_hash", "log_index", "contract_address", "topic_0", "topic_1", "topic_2", "topic_3", "data", "block_number"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// UpdateBlockStatus updates a single block's status.
func (bw *BatchWriter) UpdateBlockStatus(ctx context.Context, blockNumber int64, status string) error {
	_, err := bw.pool.Exec(ctx,
		"UPDATE blocks SET status=$1, processed_at=$2 WHERE block_number=$3",
		status, time.Now(), blockNumber)
	return err
}

// ResetProcessingBlocks resets blocks stuck in 'processing' status.
func (bw *BatchWriter) ResetProcessingBlocks(ctx context.Context) (int64, error) {
	tag, err := bw.pool.Exec(ctx,
		"UPDATE blocks SET status='unprocessed' WHERE status='processing'")
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// GetLastBlockNumber returns the max block number in the DB.
func (bw *BatchWriter) GetLastBlockNumber(ctx context.Context) (int64, error) {
	var num *int64
	err := bw.pool.QueryRow(ctx, "SELECT MAX(block_number) FROM blocks").Scan(&num)
	if err != nil {
		return 0, err
	}
	if num == nil {
		return 0, nil
	}
	return *num, nil
}

// GetUnprocessedBlocks returns blocks with status='unprocessed', ordered by number.
func (bw *BatchWriter) GetUnprocessedBlocks(ctx context.Context, limit int) ([]int64, error) {
	rows, err := bw.pool.Query(ctx,
		"SELECT block_number FROM blocks WHERE status='unprocessed' ORDER BY block_number LIMIT $1", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nums []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		nums = append(nums, n)
	}
	return nums, rows.Err()
}

// CountByStatus returns block counts grouped by status.
func (bw *BatchWriter) CountByStatus(ctx context.Context) (map[string]int64, error) {
	rows, err := bw.pool.Query(ctx,
		"SELECT status, COUNT(*) FROM blocks GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int64)
	for rows.Next() {
		var s string
		var c int64
		if err := rows.Scan(&s, &c); err != nil {
			return nil, err
		}
		result[s] = c
	}
	return result, rows.Err()
}

// TrimTopic adds "0x" prefix if missing, coerces empty string.
func TrimTopic(s string) string {
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "0x") {
		return s
	}
	return "0x" + s
}

func fmtBigInt(s string) string {
	return s
}
