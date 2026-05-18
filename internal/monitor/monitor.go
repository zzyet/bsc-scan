package monitor

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5/pgxpool"

	"bsc-scan/internal/store"
)

// Monitor checks transactions against monitored contracts and extracts relevant data.
type Monitor struct {
	db          *pgxpool.Pool
	batchWriter *store.BatchWriter
	contracts   map[string]bool // address -> active
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

func New(db *pgxpool.Pool, bw *store.BatchWriter) *Monitor {
	return &Monitor{
		db:          db,
		batchWriter: bw,
		contracts:   make(map[string]bool),
	}
}

func (m *Monitor) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	if err := m.syncContracts(); err != nil {
		log.Printf("[monitor] Initial sync warning: %v", err)
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.syncContracts()
			}
		}
	}()

	log.Printf("[monitor] Started, %d contracts loaded", len(m.contracts))
	return nil
}

func (m *Monitor) Close() error {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	log.Printf("[monitor] Stopped")
	return nil
}

func (m *Monitor) Name() string { return "ContractMonitor" }
func (m *Monitor) Ready() error     { return nil }
func (m *Monitor) HealthReport() map[string]error { return map[string]error{"monitor": nil} }

func (m *Monitor) syncContracts() error {
	rows, err := m.db.Query(m.ctx,
		"SELECT address, active FROM monitored_contracts")
	if err != nil {
		return fmt.Errorf("query contracts: %w", err)
	}
	defer rows.Close()

	newContracts := make(map[string]bool)
	for rows.Next() {
		var addr string
		var active bool
		if err := rows.Scan(&addr, &active); err != nil {
			return err
		}
		newContracts[addr] = active
	}

	m.mu.Lock()
	m.contracts = newContracts
	m.mu.Unlock()

	log.Printf("[monitor] Synced %d contracts", len(newContracts))
	return nil
}

// MatchAndParse checks if a transaction involves a monitored contract.
// Returns parsed contract transaction and events if matched, nil otherwise.
func (m *Monitor) MatchAndParse(tx *types.Transaction, receipt *types.Receipt, blockTime uint64) (*store.ContractTx, []store.ContractEvent) {
	m.mu.RLock()
	contracts := m.contracts
	m.mu.RUnlock()

	if len(contracts) == 0 {
		return nil, nil
	}

	toAddr := ""
	if tx.To() != nil {
		toAddr = tx.To().Hex()
	}

	fromAddr := ""
	signer := types.NewLondonSigner(tx.ChainId())
	if sender, err := types.Sender(signer, tx); err == nil {
		fromAddr = sender.Hex()
	}

	// Check if from or to matches any monitored contract
	matchedAddr := ""
	if contracts[toAddr] {
		matchedAddr = toAddr
	} else if contracts[fromAddr] {
		matchedAddr = fromAddr
	}

	if matchedAddr == "" {
		return nil, nil
	}

	// Parse method selector from input data
	methodSelector := ""
	if len(tx.Data()) >= 4 {
		methodSelector = fmt.Sprintf("0x%x", tx.Data()[:4])
	}

	ct := &store.ContractTx{
		TxHash:          tx.Hash().Hex(),
		ContractAddress: matchedAddr,
		BlockNumber:     receipt.BlockNumber.Int64(),
		BlockTimestamp:  int64(blockTime),
		FromAddr:        fromAddr,
		ToAddr:          toAddr,
		Value:           tx.Value().String(),
		GasUsed:         int64(receipt.GasUsed),
		GasPrice:        tx.GasPrice().String(),
		Status:          int16(receipt.Status),
		MethodSelector:  methodSelector,
		InputData:       fmt.Sprintf("0x%x", tx.Data()),
	}

	// Extract contract events
	var events []store.ContractEvent
	for _, lg := range receipt.Logs {
		if lg.Address.Hex() != matchedAddr {
			// Only record events from the matched contract
			continue
		}
		topics := [4]string{}
		for i, t := range lg.Topics {
			if i < 4 {
				topics[i] = t.Hex()
			}
		}
		events = append(events, store.ContractEvent{
			TxHash:          tx.Hash().Hex(),
			LogIndex:        int(lg.Index),
			ContractAddress: lg.Address.Hex(),
			Topic0:          topics[0],
			Topic1:          topics[1],
			Topic2:          topics[2],
			Topic3:          topics[3],
			Data:            fmt.Sprintf("0x%x", lg.Data),
			BlockNumber:     int64(lg.BlockNumber),
		})
	}

	// Check for Transfer event and log
	for _, lg := range receipt.Logs {
		transferTopic := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
		if len(lg.Topics) > 0 && lg.Topics[0] == transferTopic {
			from := common.BytesToAddress(lg.Topics[1].Bytes()).Hex()
			to := common.BytesToAddress(lg.Topics[2].Bytes()).Hex()
			value := new(big.Int).SetBytes(lg.Data)
			log.Printf("[monitor] Transfer: %s → %s value=%s tx=%s (contract=%s)",
				from, to, value.String(), tx.Hash().Hex()[:16], matchedAddr)
		}
	}

	return ct, events
}
