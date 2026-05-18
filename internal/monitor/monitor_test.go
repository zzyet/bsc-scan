package monitor

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// testKey is a deterministic private key for testing.
// Address: 0x96216849c49358B10257cb55b28eA603c874b05E
var testKey, _ = crypto.HexToECDSA("b71c71a67e1177bf5b3b09a13c3e7c4e0b5b1a8b6c7d8e9f0a1b2c3d4e5f6789")

func testKeyAddr() common.Address {
	return crypto.PubkeyToAddress(testKey.PublicKey)
}

// mockSignedTx creates a real EIP-1559 signed transaction from testKey.
func mockSignedTx(to string, data []byte, chainID int64) *types.Transaction {
	var toAddr *common.Address
	if to != "" {
		addr := common.HexToAddress(to)
		toAddr = &addr
	}
	txData := types.DynamicFeeTx{
		ChainID:   big.NewInt(chainID),
		Nonce:     1,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(30000000000),
		Gas:       21000,
		To:        toAddr,
		Value:     big.NewInt(0),
		Data:      data,
	}
	signer := types.NewLondonSigner(big.NewInt(chainID))
	signedTx, _ := types.SignTx(types.NewTx(&txData), signer, testKey)
	return signedTx
}

func mockReceipt(status uint64, gasUsed uint64, logs []*types.Log) *types.Receipt {
	return &types.Receipt{
		Status:  status,
		GasUsed: gasUsed,
		Logs:    logs,
	}
}

func mockLog(address string, topics []common.Hash, data []byte) *types.Log {
	return &types.Log{
		Address: common.HexToAddress(address),
		Topics:  topics,
		Data:    data,
	}
}

func TestMonitor_MatchAndParse_NoContracts(t *testing.T) {
	m := &Monitor{contracts: make(map[string]bool)}

	tx := mockSignedTx("0x1234567890123456789012345678901234567890", nil, 56)
	receipt := mockReceipt(1, 21000, nil)

	ct, ce := m.MatchAndParse(tx, receipt, 1000)
	if ct != nil {
		t.Error("Expected nil when no contracts monitored")
	}
	if ce != nil {
		t.Error("Expected nil events when no contracts monitored")
	}
}

func TestMonitor_MatchAndParse_FromMatch(t *testing.T) {
	fromAddr := testKeyAddr().Hex()

	m := &Monitor{
		contracts: map[string]bool{
			fromAddr: true,
		},
	}

	txData := []byte{0xa9, 0x05, 0x9c, 0xbb, 0x00, 0x00}
	tx := mockSignedTx("0x9999999999999999999999999999999999999999", txData, 56)
	receipt := mockReceipt(1, 50000, nil)
	receipt.BlockNumber = big.NewInt(100)

	ct, ce := m.MatchAndParse(tx, receipt, 1234567890)
	if ct == nil {
		t.Fatal("Expected contract tx from matched from address")
	}
	if ct.ContractAddress != fromAddr {
		t.Errorf("Expected ContractAddress=%s, got %s", fromAddr, ct.ContractAddress)
	}
	if ct.FromAddr != fromAddr {
		t.Errorf("Expected FromAddr=%s, got %s", fromAddr, ct.FromAddr)
	}
	if ct.MethodSelector != "0xa9059cbb" {
		t.Errorf("Expected MethodSelector=0xa9059cbb, got %s", ct.MethodSelector)
	}
	if ct.BlockNumber != 100 {
		t.Errorf("Expected BlockNumber=100, got %d", ct.BlockNumber)
	}
	if ct.Status != 1 {
		t.Errorf("Expected Status=1, got %d", ct.Status)
	}
	if len(ce) != 0 {
		t.Errorf("Expected 0 events, got %d", len(ce))
	}
}

func TestMonitor_MatchAndParse_ToMatch(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"

	m := &Monitor{
		contracts: map[string]bool{contractAddr: true},
	}

	tx := mockSignedTx(contractAddr, nil, 56)
	receipt := mockReceipt(1, 21000, nil)
	receipt.BlockNumber = big.NewInt(200)

	ct, _ := m.MatchAndParse(tx, receipt, 2000000)
	if ct == nil {
		t.Fatal("Expected contract tx from matched to address")
	}
	if ct.ContractAddress != contractAddr {
		t.Errorf("Expected ContractAddress=%s, got %s", contractAddr, ct.ContractAddress)
	}
	if ct.ToAddr != contractAddr {
		t.Errorf("Expected ToAddr=%s, got %s", contractAddr, ct.ToAddr)
	}
}

func TestMonitor_MatchAndParse_NoMatch(t *testing.T) {
	m := &Monitor{
		contracts: map[string]bool{
			"0x1234567890123456789012345678901234567890": true,
		},
	}

	tx := mockSignedTx("0x9999999999999999999999999999999999999999", nil, 56)
	receipt := mockReceipt(1, 21000, nil)

	ct, ce := m.MatchAndParse(tx, receipt, 1000)
	if ct != nil {
		t.Error("Expected nil when no address matches")
	}
	if ce != nil {
		t.Error("Expected nil events when no address matches")
	}
}

func TestMonitor_MatchAndParse_WithEvents(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	transferTopic := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	fromTopic := common.HexToHash("0x000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	toTopic := common.HexToHash("0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	m := &Monitor{
		contracts: map[string]bool{contractAddr: true},
	}

	event1 := mockLog(contractAddr, []common.Hash{transferTopic, fromTopic, toTopic},
		common.LeftPadBytes(big.NewInt(1000).Bytes(), 32))
	event2 := mockLog("0x9999999999999999999999999999999999999999", []common.Hash{transferTopic, fromTopic, toTopic},
		common.LeftPadBytes(big.NewInt(500).Bytes(), 32))

	tx := mockSignedTx(contractAddr, nil, 56)
	receipt := mockReceipt(1, 100000, []*types.Log{event1, event2})
	receipt.BlockNumber = big.NewInt(300)

	ct, ce := m.MatchAndParse(tx, receipt, 3000000)
	if ct == nil {
		t.Fatal("Expected contract tx")
	}
	if len(ce) != 1 {
		t.Errorf("Expected 1 event (only from monitored contract), got %d", len(ce))
	}
	if ce[0].ContractAddress != contractAddr {
		t.Errorf("Expected event ContractAddress=%s, got %s", contractAddr, ce[0].ContractAddress)
	}
	if ce[0].Topic0 != transferTopic.Hex() {
		t.Errorf("Expected Topic0=%s, got %s", transferTopic.Hex(), ce[0].Topic0)
	}
}

func TestMonitor_MatchAndParse_MethodSelector(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"

	m := &Monitor{
		contracts: map[string]bool{contractAddr: true},
	}

	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{"transfer", []byte{0xa9, 0x05, 0x9c, 0xbb, 0x00, 0x00, 0x00, 0x01}, "0xa9059cbb"},
		{"approve", []byte{0x09, 0x5e, 0xa7, 0xb3}, "0x095ea7b3"},
		{"empty_data", []byte{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := mockSignedTx(contractAddr, tt.data, 56)
			receipt := mockReceipt(1, 50000, nil)
			receipt.BlockNumber = big.NewInt(100)

			ct, _ := m.MatchAndParse(tx, receipt, 1000)
			if ct == nil {
				t.Fatal("Expected contract tx")
			}
			if ct.MethodSelector != tt.expected {
				t.Errorf("Expected MethodSelector=%s, got %s", tt.expected, ct.MethodSelector)
			}
		})
	}
}

func TestMonitor_MatchAndParse_BothMatchPrefersTo(t *testing.T) {
	// When both from and to are monitored contracts, to takes priority
	contractAddr := "0x1234567890123456789012345678901234567890"
	fromAddr := testKeyAddr().Hex()

	m := &Monitor{
		contracts: map[string]bool{
			contractAddr: true,
			fromAddr:     true,
		},
	}

	tx := mockSignedTx(contractAddr, nil, 56)
	receipt := mockReceipt(1, 21000, nil)
	receipt.BlockNumber = big.NewInt(400)

	ct, _ := m.MatchAndParse(tx, receipt, 4000000)
	if ct == nil {
		t.Fatal("Expected contract tx when both matched")
	}
	// Should match 'to' (checked first in code)
	if ct.ContractAddress != contractAddr {
		t.Errorf("Expected ContractAddress (to)=%s, got %s", contractAddr, ct.ContractAddress)
	}
}

func TestMonitor_ServiceInterface(t *testing.T) {
	m := New(nil, nil)
	if m.Name() != "ContractMonitor" {
		t.Errorf("Expected Name()='ContractMonitor', got '%s'", m.Name())
	}
	if err := m.Ready(); err != nil {
		t.Errorf("Ready() should return nil, got %v", err)
	}
	report := m.HealthReport()
	if report["monitor"] != nil {
		t.Errorf("HealthReport monitor should be nil, got %v", report["monitor"])
	}
}

func TestMonitor_New(t *testing.T) {
	m := New(nil, nil)
	if m == nil {
		t.Fatal("New() returned nil")
	}
	if m.contracts == nil {
		t.Error("contracts map should be initialized")
	}
	if len(m.contracts) != 0 {
		t.Errorf("contracts should be empty, got %d", len(m.contracts))
	}
}
