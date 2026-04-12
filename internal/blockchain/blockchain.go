package blockchain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"qbitcoin/internal/crypto"

	"github.com/google/uuid"
)

// Transaction types
const (
	TxTransfer = "TRANSFER"
	TxStake    = "STAKE"
	TxUnstake  = "UNSTAKE"
	TxContract = "CONTRACT"
	TxReward   = "REWARD"
)

// Transaction represents a qBitcoin transaction.
type Transaction struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	From      string            `json:"from"`
	To        string            `json:"to"`
	Amount    uint64            `json:"amount"`
	Fee       uint64            `json:"fee"`
	Data      []byte            `json:"data,omitempty"`
	Timestamp int64             `json:"timestamp"`
	Signature []byte            `json:"signature,omitempty"`
	PublicKey []byte            `json:"public_key,omitempty"`
	Nonce     uint64            `json:"nonce"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// NewTransaction creates a new transaction.
func NewTransaction(txType, from, to string, amount, fee uint64) *Transaction {
	return &Transaction{
		ID:        uuid.New().String(),
		Type:      txType,
		From:      from,
		To:        to,
		Amount:    amount,
		Fee:       fee,
		Timestamp: time.Now().UnixNano(),
	}
}

// Hash returns the transaction hash. Includes Data so contract payloads affect the hash.
func (tx *Transaction) Hash() string {
	data, _ := json.Marshal(struct {
		Type      string `json:"type"`
		From      string `json:"from"`
		To        string `json:"to"`
		Amount    uint64 `json:"amount"`
		Fee       uint64 `json:"fee"`
		Data      []byte `json:"data,omitempty"`
		Timestamp int64  `json:"timestamp"`
		Nonce     uint64 `json:"nonce"`
	}{tx.Type, tx.From, tx.To, tx.Amount, tx.Fee, tx.Data, tx.Timestamp, tx.Nonce})
	return crypto.BLAKE3HashHex(data)
}

// Sign signs the transaction with a private key.
func (tx *Transaction) Sign(privateKey, publicKey []byte) error {
	msgHash := []byte(tx.Hash())
	sig, err := crypto.Sign(privateKey, msgHash)
	if err != nil {
		return err
	}
	tx.Signature = sig
	tx.PublicKey = publicKey
	return nil
}

// Verify verifies the transaction signature.
func (tx *Transaction) Verify() bool {
	if tx.From == "COINBASE" {
		return true
	}
	if tx.Signature == nil || tx.PublicKey == nil {
		return false
	}
	addr := crypto.AddressFromPublicKey(tx.PublicKey)
	if addr != tx.From {
		return false
	}
	return crypto.Verify(tx.PublicKey, []byte(tx.Hash()), tx.Signature)
}

// Block represents a blockchain block.
type Block struct {
	Index        uint64         `json:"index"`
	Timestamp    int64          `json:"timestamp"`
	Transactions []*Transaction `json:"transactions"`
	PrevHash     string         `json:"prev_hash"`
	Hash         string         `json:"hash"`
	Validator    string         `json:"validator"`
	Stake        uint64         `json:"stake"`
	Signature    []byte         `json:"signature,omitempty"`
	StateRoot    string         `json:"state_root"`
}

// ComputeHash computes the block hash using a Merkle-like concatenation with separator.
func (b *Block) ComputeHash() string {
	var sb strings.Builder
	for _, tx := range b.Transactions {
		sb.WriteString(tx.Hash())
		sb.WriteByte('|')
	}
	data := fmt.Sprintf("%d|%d|%s|%s|%s|%d",
		b.Index, b.Timestamp, sb.String(), b.PrevHash, b.Validator, b.Stake)
	return crypto.BLAKE3HashHex([]byte(data))
}

// Validator represents a PoS validator.
type Validator struct {
	Address    string `json:"address"`
	PublicKey  []byte `json:"public_key"`
	Stake      uint64 `json:"stake"`
	Active     bool   `json:"active"`
	LastReward int64  `json:"last_reward"`
}

// State holds account balances, nonces, and stakes.
type State struct {
	mu       sync.RWMutex
	Balances map[string]uint64 `json:"balances"`
	Nonces   map[string]uint64 `json:"nonces"`
	Stakes   map[string]uint64 `json:"stakes"`
}

// NewState creates a new empty state.
func NewState() *State {
	return &State{
		Balances: make(map[string]uint64),
		Nonces:   make(map[string]uint64),
		Stakes:   make(map[string]uint64),
	}
}

func (s *State) GetBalance(addr string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Balances[addr]
}

func (s *State) SetBalance(addr string, amount uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Balances[addr] = amount
}

func (s *State) GetNonce(addr string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Nonces[addr]
}

func (s *State) IncrNonce(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Nonces[addr]++
}

func (s *State) GetStake(addr string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Stakes[addr]
}

func (s *State) SetStake(addr string, amount uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Stakes[addr] = amount
}

// Hash returns a deterministic hash of the full state (balances + nonces + stakes).
func (s *State) Hash() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	combined := struct {
		Balances map[string]uint64 `json:"b"`
		Nonces   map[string]uint64 `json:"n"`
		Stakes   map[string]uint64 `json:"s"`
	}{s.Balances, s.Nonces, s.Stakes}
	data, _ := json.Marshal(combined)
	return crypto.BLAKE3HashHex(data)
}

// Blockchain is the main chain structure.
type Blockchain struct {
	mu         sync.RWMutex
	Blocks     []*Block              `json:"blocks"`
	State      *State                `json:"state"`
	Validators map[string]*Validator `json:"validators"`
	Mempool    []*Transaction        `json:"-"`
	mempoolMu  sync.Mutex

	BlockReward  uint64
	MinStake     uint64
	BlockTime    time.Duration
	MaxBlockSize int
}

// NewBlockchain creates a genesis blockchain.
func NewBlockchain() *Blockchain {
	bc := &Blockchain{
		Blocks:       make([]*Block, 0),
		State:        NewState(),
		Validators:   make(map[string]*Validator),
		Mempool:      make([]*Transaction, 0),
		BlockReward:  50_000_000,
		MinStake:     1_000_000,
		BlockTime:    5 * time.Second,
		MaxBlockSize: 1000,
	}
	bc.createGenesis()
	return bc
}

func (bc *Blockchain) createGenesis() {
	genesisTx := &Transaction{
		ID:        uuid.New().String(),
		Type:      TxReward,
		From:      "COINBASE",
		To:        "genesis",
		Amount:    21_000_000 * 100_000_000,
		Timestamp: time.Now().UnixNano(),
	}
	// Apply genesis transaction to state
	bc.State.Balances["genesis"] += genesisTx.Amount

	genesis := &Block{
		Index:        0,
		Timestamp:    time.Now().UnixNano(),
		Transactions: []*Transaction{genesisTx},
		PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
		Validator:    "genesis",
	}
	genesis.StateRoot = bc.State.Hash()
	genesis.Hash = genesis.ComputeHash()
	bc.Blocks = append(bc.Blocks, genesis)
}

// AddTransaction adds a transaction to the mempool.
// TxReward transactions are rejected — they are created internally only.
func (bc *Blockchain) AddTransaction(tx *Transaction) error {
	if tx.Type == TxReward {
		return errors.New("reward transactions cannot be submitted externally")
	}
	if !tx.Verify() {
		return errors.New("invalid transaction signature")
	}
	balance := bc.State.GetBalance(tx.From)
	if tx.Type == TxTransfer && balance < tx.Amount+tx.Fee {
		return errors.New("insufficient balance")
	}

	bc.mempoolMu.Lock()
	defer bc.mempoolMu.Unlock()
	bc.Mempool = append(bc.Mempool, tx)
	return nil
}

// Balance returns the balance of an address (safe accessor for external packages).
func (bc *Blockchain) Balance(addr string) uint64 {
	return bc.State.GetBalance(addr)
}

// SelectValidator selects a validator via weighted random PoS using crypto/rand.
// Validators are sorted by address for determinism before selection.
func (bc *Blockchain) SelectValidator() *Validator {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	// Collect active validators in deterministic order
	active := make([]*Validator, 0, len(bc.Validators))
	for _, v := range bc.Validators {
		if v.Active && v.Stake >= bc.MinStake {
			active = append(active, v)
		}
	}
	if len(active) == 0 {
		return nil
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i].Address < active[j].Address
	})

	var totalStake uint64
	for _, v := range active {
		totalStake += v.Stake
	}

	// Cryptographically secure random target
	targetBig, err := crypto.RandomBigInt(new(big.Int).SetUint64(totalStake))
	if err != nil {
		return nil
	}
	target := targetBig.Uint64()

	var cumulative uint64
	for _, v := range active {
		cumulative += v.Stake
		if cumulative > target {
			// Return a copy to avoid races on the pointer
			cp := *v
			return &cp
		}
	}
	cp := *active[len(active)-1]
	return &cp
}

// MintBlock creates a new block with pending transactions.
func (bc *Blockchain) MintBlock(validator *Validator) (*Block, error) {
	// Drain mempool first (before acquiring bc.mu to avoid lock inversion)
	bc.mempoolMu.Lock()
	limit := bc.MaxBlockSize
	if len(bc.Mempool) < limit {
		limit = len(bc.Mempool)
	}
	txs := make([]*Transaction, limit)
	copy(txs, bc.Mempool[:limit])
	bc.Mempool = bc.Mempool[limit:]
	bc.mempoolMu.Unlock()

	// Prepend reward transaction
	reward := &Transaction{
		ID:        uuid.New().String(),
		Type:      TxReward,
		From:      "COINBASE",
		To:        validator.Address,
		Amount:    bc.BlockReward,
		Timestamp: time.Now().UnixNano(),
	}
	allTxs := make([]*Transaction, 0, len(txs)+1)
	allTxs = append(allTxs, reward)
	allTxs = append(allTxs, txs...)

	bc.mu.Lock()
	defer bc.mu.Unlock()

	prev := bc.Blocks[len(bc.Blocks)-1]
	block := &Block{
		Index:        uint64(len(bc.Blocks)),
		Timestamp:    time.Now().UnixNano(),
		Transactions: allTxs,
		PrevHash:     prev.Hash,
		Validator:    validator.Address,
		Stake:        validator.Stake,
	}

	for _, tx := range allTxs {
		bc.applyTx(tx)
	}

	block.StateRoot = bc.State.Hash()
	block.Hash = block.ComputeHash()
	bc.Blocks = append(bc.Blocks, block)
	return block, nil
}

// applyTx applies a transaction to the state using mutex-guarded accessors.
func (bc *Blockchain) applyTx(tx *Transaction) {
	switch tx.Type {
	case TxReward:
		bal := bc.State.GetBalance(tx.To)
		bc.State.SetBalance(tx.To, bal+tx.Amount)

	case TxTransfer:
		from := bc.State.GetBalance(tx.From)
		if from >= tx.Amount+tx.Fee {
			bc.State.SetBalance(tx.From, from-tx.Amount-tx.Fee)
			to := bc.State.GetBalance(tx.To)
			bc.State.SetBalance(tx.To, to+tx.Amount)
			bc.State.IncrNonce(tx.From)
		}

	case TxStake:
		from := bc.State.GetBalance(tx.From)
		if from >= tx.Amount {
			bc.State.SetBalance(tx.From, from-tx.Amount)
			staked := bc.State.GetStake(tx.From)
			bc.State.SetStake(tx.From, staked+tx.Amount)
			if v, ok := bc.Validators[tx.From]; ok {
				v.Stake += tx.Amount
			}
		}

	case TxUnstake:
		staked := bc.State.GetStake(tx.From)
		if staked >= tx.Amount {
			bc.State.SetStake(tx.From, staked-tx.Amount)
			bal := bc.State.GetBalance(tx.From)
			bc.State.SetBalance(tx.From, bal+tx.Amount)
			if v, ok := bc.Validators[tx.From]; ok {
				v.Stake -= tx.Amount
				if v.Stake < bc.MinStake {
					v.Active = false
				}
			}
		}
	}
}

// RegisterValidator registers a new validator, deducting stake from balance.
func (bc *Blockchain) RegisterValidator(address string, publicKey []byte, stake uint64) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if stake < bc.MinStake {
		return fmt.Errorf("stake %d below minimum %d", stake, bc.MinStake)
	}
	bal := bc.State.GetBalance(address)
	if bal < stake {
		return fmt.Errorf("insufficient balance %d for stake %d", bal, stake)
	}

	bc.State.SetBalance(address, bal-stake)
	existing := bc.State.GetStake(address)
	bc.State.SetStake(address, existing+stake)

	bc.Validators[address] = &Validator{
		Address:   address,
		PublicKey: publicKey,
		Stake:     stake,
		Active:    true,
	}
	return nil
}

// GetBlock returns a block by index.
func (bc *Blockchain) GetBlock(index uint64) (*Block, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if index >= uint64(len(bc.Blocks)) {
		return nil, errors.New("block not found")
	}
	return bc.Blocks[index], nil
}

// Height returns the current chain height.
func (bc *Blockchain) Height() uint64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return uint64(len(bc.Blocks))
}

// ValidateChain validates chain integrity: hash linkage, block hash correctness,
// and timestamp monotonicity.
func (bc *Blockchain) ValidateChain() bool {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	for i := 1; i < len(bc.Blocks); i++ {
		curr := bc.Blocks[i]
		prev := bc.Blocks[i-1]
		if curr.PrevHash != prev.Hash {
			return false
		}
		if curr.Hash != curr.ComputeHash() {
			return false
		}
		if curr.Timestamp < prev.Timestamp {
			return false
		}
	}
	return true
}

// MempoolSize returns the number of pending transactions.
func (bc *Blockchain) MempoolSize() int {
	bc.mempoolMu.Lock()
	defer bc.mempoolMu.Unlock()
	return len(bc.Mempool)
}
