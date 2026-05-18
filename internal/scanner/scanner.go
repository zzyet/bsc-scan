package scanner

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5/pgxpool"

	"bsc-scan/internal/config"
	"bsc-scan/internal/endpoint"
	"bsc-scan/internal/monitor"
	"bsc-scan/internal/store"
)

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

	log.Printf("[scanner] Started (start_block=%d, workers=%d, batch=%d)", s.cfg.StartBlock, s.cfg.WorkerCount, s.cfg.BatchSize)
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

func (s *Scanner) Name() string { return "BlockScanner" }
func (s *Scanner) Ready() error     { return nil }
func (s *Scanner) HealthReport() map[string]error { return map[string]error{"scanner": nil} }

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

	client, release, err := s.acquireClient(ctx)
	if err != nil {
		return
	}
	defer release()

	chainLatest, err := client.BlockNumber(ctx)
	if err != nil {
		release() // Release with failure
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
			// Start from latest block (0 = catch-up mode)
			start = int64(chainLatest)
		}
	}

	if start > int64(chainLatest) {
		return
	}

	end := start + 99
	if end > int64(chainLatest) {
		end = int64(chainLatest)
	}

	log.Printf("[scanner] Fetching blocks %d → %d", start, end)

	var blocks []store.Block
	for num := start; num <= end; num++ {
		block, err := client.BlockByNumber(ctx, big.NewInt(num))
		if err != nil {
			log.Printf("[scanner] Failed to fetch block %d: %v", num, err)
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

	sem := make(chan struct{}, s.cfg.WorkerCount)
	var wg sync.WaitGroup

	for _, num := range blockNums {
		select {
		case <-s.ctx.Done():
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(blockNum int64) {
			defer wg.Done()
			defer func() { <-sem }()
			s.processBlock(blockNum)
		}(num)
	}
	wg.Wait()
}

func (s *Scanner) processBlock(blockNum int64) {
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()

	s.batchWriter.UpdateBlockStatus(ctx, blockNum, "processing")

	client, release, err := s.acquireClient(ctx)
	if err != nil {
		s.batchWriter.UpdateBlockStatus(ctx, blockNum, "unprocessed")
		return
	}
	defer release()

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

	chainID := big.NewInt(56) // BSC mainnet
	signer := types.NewLondonSigner(chainID)

	var txs []store.Tx
	var allLogs []store.EventLog
	var contractTxs []store.ContractTx
	var contractEvents []store.ContractEvent

	for _, tx := range block.Transactions() {
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

		receipt, err := client.TransactionReceipt(ctx, tx.Hash())
		if err == nil {
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
		if s.monitor != nil {
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

// acquireClient gets a client from the endpoint manager lease pool.
func (s *Scanner) acquireClient(ctx context.Context) (*ethclient.Client, func(), error) {
	lease, err := s.epMgr.AcquireEndpoint(ctx)
	if err != nil {
		return nil, func() {}, fmt.Errorf("acquire endpoint: %w", err)
	}

	client, err := ethclient.DialContext(ctx, lease.Config().URL)
	if err != nil {
		lease.ReportFailure()
		return nil, func() {}, err
	}

	release := func() {
		lease.ReportSuccess()
		client.Close()
	}
	return client, release, nil
}
