package scanner

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5/pgxpool"

	"bsc-scan/internal/config"
	"bsc-scan/internal/endpoint"
	"bsc-scan/internal/monitor"
	"bsc-scan/internal/store"
)

// receiptResult holds either a receipt or an error for a single tx.
type receiptResult struct {
	receipt *types.Receipt
	err     error
	txHash  string // to track which tx it belongs to
}

// Scanner implements services.Service, coordinating fetch and process phases.
type Scanner struct {
	db          *pgxpool.Pool
	batchWriter *store.BatchWriter
	epMgr       *endpoint.Manager
	cfg         config.ScannerConfig
	monitor     *monitor.Monitor
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// New creates a new Scanner.
func New(db *pgxpool.Pool, bw *store.BatchWriter, epMgr *endpoint.Manager, cfg config.ScannerConfig) *Scanner {
	return &Scanner{
		db:          db,
		batchWriter: bw,
		epMgr:       epMgr,
		cfg:         cfg,
	}
}

// SetMonitor sets the contract monitor reference after construction.
func (s *Scanner) SetMonitor(m *monitor.Monitor) {
	s.monitor = m
}

func (s *Scanner) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	reset, _ := s.batchWriter.ResetProcessingBlocks(s.ctx)
	if reset > 0 {
		log.Printf("[scanner] Reset %d blocks from processing to unprocessed", reset)
	}

	s.wg.Add(1)
	go s.fetchLoop()

	s.wg.Add(1)
	go s.processLoop()

	fetchSize := s.cfg.FetchBatchSize
	if fetchSize == 0 {
		fetchSize = 100
	}
	log.Printf("[scanner] Started (start_block=%d, workers=%d, batch=%d, fetch_batch=%d)",
		s.cfg.StartBlock, s.cfg.WorkerCount, s.cfg.BatchSize, fetchSize)
	return nil
}

func (s *Scanner) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	log.Printf("[scanner] Stopped")
	return nil
}

func (s *Scanner) Name() string                { return "BlockScanner" }
func (s *Scanner) Ready() error                { return nil }
func (s *Scanner) HealthReport() map[string]error { return map[string]error{"scanner": nil} }

// ---------------------------------------------------------------------------
// Fetch phase: parallel block metadata fetching
// ---------------------------------------------------------------------------

func (s *Scanner) fetchLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.fetchBlocks()
		}
	}
}

func (s *Scanner) fetchBlocks() {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	client, lease, closeFn, err := s.acquireClient(ctx)
	if err != nil {
		return
	}
	defer closeFn()
	defer lease.ReportSuccess()

	chainLatest, err := client.BlockNumber(ctx)
	if err != nil {
		lease.ReportFailure()
		log.Printf("[scanner] Failed to get chain block number: %v", err)
		return
	}

	lastDB, err := s.batchWriter.GetLastBlockNumber(ctx)
	if err != nil {
		log.Printf("[scanner] Failed to get DB block number: %v", err)
		return
	}

	start := lastDB + 1
	if lastDB == 0 {
		if s.cfg.StartBlock > 0 {
			start = s.cfg.StartBlock
		} else {
			start = int64(chainLatest)
		}
	}

	if start > int64(chainLatest) {
		return
	}

	fetchSize := s.cfg.FetchBatchSize
	if fetchSize <= 0 {
		fetchSize = 100
	}

	end := start + int64(fetchSize) - 1
	if end > int64(chainLatest) {
		end = int64(chainLatest)
	}

	log.Printf("[scanner] Fetching blocks %d → %d (%d blocks)", start, end, end-start+1)

	var blocks []store.Block
	for num := start; num <= end; num++ {
		block, err := client.BlockByNumber(ctx, big.NewInt(num))
		if err != nil {
			log.Printf("[scanner] Failed to fetch block %d: %v", num, err)
			// If we hit rate limiting, report failure, switch endpoint and retry
			if isRateLimit(err) {
				lease.ReportFailure()
				closeFn()
				client, lease, closeFn, err = s.acquireClient(ctx)
				if err != nil {
					return
				}
				continue
			}
			continue
		}
		txCount := len(block.Transactions())
		blocks = append(blocks, store.Block{
			Number:     block.Number().Int64(),
			Hash:       block.Hash().Hex(),
			ParentHash: block.ParentHash().Hex(),
			Timestamp:  int64(block.Time()),
			Miner:      block.Coinbase().Hex(),
			GasUsed:    int64(block.GasUsed()),
			GasLimit:   int64(block.GasLimit()),
			TxCount:    txCount,
		})
	}

	if len(blocks) > 0 {
		if err := s.batchWriter.InsertBlocksBatch(ctx, blocks); err != nil {
			log.Printf("[scanner] Failed to batch insert blocks: %v", err)
		}
	}
}

// isRateLimit checks if the error indicates rate limiting (403/429).
func isRateLimit(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "403") || strings.Contains(msg, "429") ||
		strings.Contains(msg, "Forbidden") || strings.Contains(msg, "Too Many Requests")
}

// ---------------------------------------------------------------------------
// Process phase: parallel receipt fetching + batch RPC + connection reuse
// ---------------------------------------------------------------------------

func (s *Scanner) processLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.processBatch()
		}
	}
}

func (s *Scanner) processBatch() {
	blockNums, err := s.batchWriter.GetUnprocessedBlocks(s.ctx, s.cfg.BatchSize)
	if err != nil {
		log.Printf("[scanner] Failed to get unprocessed blocks: %v", err)
		return
	}
	if len(blockNums) == 0 {
		return
	}

	s.processBlocksParallel(blockNums)
}

func (s *Scanner) processBlocksParallel(blockNums []int64) {
	// Acquire ONE endpoint and reuse its connection for the entire batch
	ctx, cancel := context.WithTimeout(s.ctx, 120*time.Second)
	defer cancel()

	lease, err := s.epMgr.AcquireEndpoint(ctx)
	if err != nil {
		log.Printf("[scanner] Failed to acquire endpoint for batch: %v", err)
		return
	}

	client, err := ethclient.DialContext(ctx, lease.Config().URL)
	if err != nil {
		lease.ReportFailure()
		log.Printf("[scanner] Failed to dial endpoint: %v", err)
		return
	}
	defer client.Close()

	// Extract underlying rpc.Client for batch receipt calls
	rpcClient := client.Client()

	// Process blocks with worker pool
	sem := make(chan struct{}, s.cfg.WorkerCount)
	var wg sync.WaitGroup

	for _, num := range blockNums {
		select {
		case <-s.ctx.Done():
			goto done
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(blockNum int64) {
			defer wg.Done()
			defer func() { <-sem }()

			blockCtx, blockCancel := context.WithTimeout(ctx, 60*time.Second)
			defer blockCancel()

			s.processBlockFast(blockCtx, client, rpcClient, blockNum)
		}(num)
	}
done:
	wg.Wait()
	lease.ReportSuccess()
}

// processBlockFast processes a single block using a pre-dialed client.
func (s *Scanner) processBlockFast(ctx context.Context, client *ethclient.Client, rpcClient *rpc.Client, blockNum int64) {
	s.batchWriter.UpdateBlockStatus(ctx, blockNum, "processing")

	block, err := client.BlockByNumber(ctx, big.NewInt(blockNum))
	if err != nil {
		log.Printf("[scanner] Failed to fetch block %d: %v", blockNum, err)
		s.batchWriter.UpdateBlockStatus(ctx, blockNum, "unprocessed")
		return
	}

	if len(block.Transactions()) == 0 {
		s.batchWriter.UpdateBlockStatus(ctx, blockNum, "processed")
		return
	}

	// Fetch all receipts in bulk or parallel
	receipts := s.fetchReceipts(ctx, client, rpcClient, block)

	chainID := big.NewInt(56) // BSC mainnet
	signer := types.NewLondonSigner(chainID)

	var txs []store.Tx
	var allLogs []store.EventLog
	var contractTxs []store.ContractTx
	var contractEvents []store.ContractEvent

	for i, tx := range block.Transactions() {
		toAddr := ""
		if tx.To() != nil {
			toAddr = tx.To().Hex()
		}

		fromAddr := ""
		if sender, err := types.Sender(signer, tx); err == nil {
			fromAddr = sender.Hex()
		}

		txRecord := store.Tx{
			Hash:        tx.Hash().Hex(),
			BlockNumber: blockNum,
			FromAddr:    fromAddr,
			ToAddr:      toAddr,
			Value:       tx.Value().String(),
			Gas:         int64(tx.Gas()),
			GasPrice:    tx.GasPrice().String(),
			InputData:   fmt.Sprintf("0x%x", tx.Data()),
		}

		receipt := receipts[i]
		if receipt != nil {
			txRecord.Status = int16(receipt.Status)

			for _, lg := range receipt.Logs {
				topics := [4]string{}
				for i, t := range lg.Topics {
					if i < 4 {
						topics[i] = t.Hex()
					}
				}
				allLogs = append(allLogs, store.EventLog{
					TxHash:      tx.Hash().Hex(),
					BlockNumber: int64(lg.BlockNumber),
					LogIndex:    int(lg.Index),
					Address:     lg.Address.Hex(),
					Topic0:      topics[0],
					Topic1:      topics[1],
					Topic2:      topics[2],
					Topic3:      topics[3],
					Data:        fmt.Sprintf("0x%x", lg.Data),
				})
			}
		}

		txs = append(txs, txRecord)

		// Contract monitor integration
		if s.monitor != nil && receipt != nil {
			ct, ce := s.monitor.MatchAndParse(tx, receipt, block.Time())
			if ct != nil {
				contractTxs = append(contractTxs, *ct)
			}
			contractEvents = append(contractEvents, ce...)
		}
	}

	if len(txs) > 0 {
		s.batchWriter.InsertTxsBatch(ctx, txs)
	}
	if len(allLogs) > 0 {
		s.batchWriter.InsertEventLogsBatch(ctx, allLogs)
	}
	if len(contractTxs) > 0 {
		s.batchWriter.InsertContractTxsBatch(ctx, contractTxs)
	}
	if len(contractEvents) > 0 {
		s.batchWriter.InsertContractEventsBatch(ctx, contractEvents)
	}

	s.batchWriter.UpdateBlockStatus(ctx, blockNum, "processed")
}

// fetchReceipts tries to get all receipts for a block efficiently.
// Strategy: try eth_getBlockReceipts (1 RPC call), fall back to parallel TransactionReceipt calls.
func (s *Scanner) fetchReceipts(ctx context.Context, client *ethclient.Client, rpcClient *rpc.Client, block *types.Block) []*types.Receipt {
	n := len(block.Transactions())
	receipts := make([]*types.Receipt, n)

	// Strategy 1: batch RPC eth_getBlockReceipts (single call)
	if rpcClient != nil {
		var batchReceipts []*types.Receipt
		blockNumHex := fmt.Sprintf("0x%x", block.Number().Int64())
		if err := rpcClient.CallContext(ctx, &batchReceipts, "eth_getBlockReceipts", blockNumHex); err == nil {
			// Map receipts back to tx index
			txHashMap := make(map[string]int, n)
			for i, tx := range block.Transactions() {
				txHashMap[tx.Hash().Hex()] = i
			}
			for _, r := range batchReceipts {
				if idx, ok := txHashMap[r.TxHash.Hex()]; ok {
					receipts[idx] = r
				}
			}
			return receipts
		}
	}

	// Strategy 2: parallel TransactionReceipt calls
	concurrency := 20 // parallel receipt fetchers
	sem := make(chan struct{}, concurrency)

	type result struct {
		idx     int
		receipt *types.Receipt
	}
	resultCh := make(chan result, n)

	var wg sync.WaitGroup
	for i, tx := range block.Transactions() {
		select {
		case <-ctx.Done():
			goto waitDone
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(idx int, txHash string) {
			defer wg.Done()
			defer func() { <-sem }()

			r, err := client.TransactionReceipt(ctx, block.Transactions()[idx].Hash())
			if err != nil {
				// Log only first error per block to reduce noise
				return
			}
			resultCh <- result{idx: idx, receipt: r}
		}(i, tx.Hash().Hex())
	}
waitDone:
	wg.Wait()
	close(resultCh)

	for r := range resultCh {
		receipts[r.idx] = r.receipt
	}

	return receipts
}

// acquireClient gets a client from the endpoint manager lease pool.
// Returns the client, the lease (for manual reporting), and a release function.
func (s *Scanner) acquireClient(ctx context.Context) (*ethclient.Client, *endpoint.Lease, func(), error) {
	lease, err := s.epMgr.AcquireEndpoint(ctx)
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("acquire endpoint: %w", err)
	}

	client, err := ethclient.DialContext(ctx, lease.Config().URL)
	if err != nil {
		lease.ReportFailure()
		return nil, lease, func() {}, err
	}

	return client, lease, func() { client.Close() }, nil
}

// acquireClientSimple gets a client with auto-success reporting (for legacy/process code).
func (s *Scanner) acquireClientSimple(ctx context.Context) (*ethclient.Client, func(), error) {
	client, lease, closeFn, err := s.acquireClient(ctx)
	if err != nil {
		return nil, func() {}, err
	}
	release := func() {
		lease.ReportSuccess()
		closeFn()
	}
	return client, release, nil
}
