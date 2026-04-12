package contracts

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"qbitcoin/internal/crypto"

	"github.com/google/uuid"
)

// ContractType defines the type of smart contract.
type ContractType string

const (
	ContractQRC20  ContractType = "QRC20"
	ContractNFT    ContractType = "NFT"
	ContractDAO    ContractType = "DAO"
	ContractCustom ContractType = "CUSTOM"
)

// Contract represents a deployed smart contract.
type Contract struct {
	Address   string            `json:"address"`
	Type      ContractType      `json:"type"`
	Owner     string            `json:"owner"`
	CreatedAt int64             `json:"created_at"`
	state     map[string][]byte
	mu        sync.RWMutex
}

func newContract(owner string, ctype ContractType) *Contract {
	id := uuid.New().String()
	addr := "qContract" + crypto.BLAKE3HashHex([]byte(id+owner))[:32]
	return &Contract{
		Address:   addr,
		Type:      ctype,
		Owner:     owner,
		CreatedAt: time.Now().UnixNano(),
		state:     make(map[string][]byte),
	}
}

func (c *Contract) get(key string) []byte {
	return c.state[key]
}

func (c *Contract) set(key string, value []byte) {
	c.state[key] = value
}

// --- QRC20 Token ---

// QRC20 is a fungible token contract (ERC-20 equivalent).
type QRC20 struct {
	*Contract
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals uint8  `json:"decimals"`
}

// NewQRC20 deploys a new QRC20 token.
func NewQRC20(owner, name, symbol string, decimals uint8, totalSupply uint64) *QRC20 {
	c := newContract(owner, ContractQRC20)
	t := &QRC20{Contract: c, Name: name, Symbol: symbol, Decimals: decimals}
	// Mint initial supply to owner
	t.mu.Lock()
	t.set(t.balanceKey(owner), uint64ToBytes(totalSupply))
	t.set("totalSupply", uint64ToBytes(totalSupply))
	t.mu.Unlock()
	return t
}

func (t *QRC20) balanceKey(addr string) string      { return "balance:" + addr }
func (t *QRC20) allowanceKey(owner, spender string) string {
	return "allowance:" + owner + ":" + spender
}

// BalanceOf returns the token balance of an address.
func (t *QRC20) BalanceOf(addr string) uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return bytesToUint64(t.get(t.balanceKey(addr)))
}

// Transfer moves tokens from one address to another atomically.
func (t *QRC20) Transfer(from, to string, amount uint64) error {
	if from == to {
		return nil // self-transfer is a no-op, avoids deadlock
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	fromBal := bytesToUint64(t.get(t.balanceKey(from)))
	if fromBal < amount {
		return fmt.Errorf("insufficient balance: have %d, need %d", fromBal, amount)
	}
	toBal := bytesToUint64(t.get(t.balanceKey(to)))
	t.set(t.balanceKey(from), uint64ToBytes(fromBal-amount))
	t.set(t.balanceKey(to), uint64ToBytes(toBal+amount))
	return nil
}

// Approve sets the allowance for spender to spend on behalf of owner.
func (t *QRC20) Approve(owner, spender string, amount uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.set(t.allowanceKey(owner, spender), uint64ToBytes(amount))
}

// Allowance returns the approved spending amount.
func (t *QRC20) Allowance(owner, spender string) uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return bytesToUint64(t.get(t.allowanceKey(owner, spender)))
}

// TransferFrom transfers tokens on behalf of 'from', up to the approved allowance.
// The caller must pass the verified spender address (enforced by the VM execution context).
func (t *QRC20) TransferFrom(spender, from, to string, amount uint64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	allowance := bytesToUint64(t.get(t.allowanceKey(from, spender)))
	if allowance < amount {
		return errors.New("allowance exceeded")
	}
	fromBal := bytesToUint64(t.get(t.balanceKey(from)))
	if fromBal < amount {
		return errors.New("insufficient balance")
	}
	toBal := bytesToUint64(t.get(t.balanceKey(to)))
	t.set(t.balanceKey(from), uint64ToBytes(fromBal-amount))
	t.set(t.balanceKey(to), uint64ToBytes(toBal+amount))
	t.set(t.allowanceKey(from, spender), uint64ToBytes(allowance-amount))
	return nil
}

// TotalSupply returns the total token supply.
func (t *QRC20) TotalSupply() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return bytesToUint64(t.get("totalSupply"))
}

// --- NFT Contract ---

// NFTToken represents a single non-fungible token.
type NFTToken struct {
	ID       uint64            `json:"id"`
	Owner    string            `json:"owner"`
	URI      string            `json:"uri"`
	Metadata map[string]string `json:"metadata"`
}

// NFTContract is an ERC-721-like NFT contract.
// A single mutex covers both nextID and all state operations.
type NFTContract struct {
	*Contract
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
	nextID uint64
}

// NewNFTContract deploys a new NFT contract.
func NewNFTContract(owner, name, symbol string) *NFTContract {
	return &NFTContract{
		Contract: newContract(owner, ContractNFT),
		Name:     name,
		Symbol:   symbol,
	}
}

// Mint creates a new NFT and assigns it to 'to'.
func (n *NFTContract) Mint(to, uri string, metadata map[string]string) (uint64, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.nextID++
	id := n.nextID

	// Persist owner and URI separately for efficient lookup
	n.set(fmt.Sprintf("owner:%d", id), []byte(to))
	n.set(fmt.Sprintf("uri:%d", id), []byte(uri))

	// Persist metadata as JSON
	if metadata != nil {
		metaBytes := make([]byte, 0, 64)
		for k, v := range metadata {
			metaBytes = append(metaBytes, []byte(k+"="+v+";")...)
		}
		n.set(fmt.Sprintf("meta:%d", id), metaBytes)
	}
	return id, nil
}

// OwnerOf returns the owner of a token.
func (n *NFTContract) OwnerOf(tokenID uint64) (string, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	owner := n.get(fmt.Sprintf("owner:%d", tokenID))
	if owner == nil {
		return "", errors.New("token not found")
	}
	return string(owner), nil
}

// Transfer transfers a token from 'from' to 'to'.
func (n *NFTContract) Transfer(from, to string, tokenID uint64) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	owner := n.get(fmt.Sprintf("owner:%d", tokenID))
	if owner == nil {
		return errors.New("token not found")
	}
	if string(owner) != from {
		return errors.New("not token owner")
	}
	n.set(fmt.Sprintf("owner:%d", tokenID), []byte(to))
	return nil
}

// --- DAO Contract ---

// Proposal is a DAO governance proposal.
type Proposal struct {
	ID           uint64            `json:"id"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	Proposer     string            `json:"proposer"`
	CreatedAt    int64             `json:"created_at"`
	DeadlineAt   int64             `json:"deadline_at"`
	VotesFor     uint64            `json:"votes_for"`
	VotesAgainst uint64            `json:"votes_against"`
	Executed     bool              `json:"executed"`
	Params       map[string]string `json:"params,omitempty"`
}

// DAOContract is a governance DAO contract.
type DAOContract struct {
	*Contract
	Name           string                  `json:"name"`
	QuorumThreshold uint64                 `json:"quorum_threshold"`
	proposals      map[uint64]*Proposal
	votes          map[string]map[uint64]bool
	nextID         uint64
}

// NewDAOContract deploys a new DAO contract with a quorum threshold.
func NewDAOContract(owner, name string) *DAOContract {
	return &DAOContract{
		Contract:        newContract(owner, ContractDAO),
		Name:            name,
		QuorumThreshold: 1, // set higher in production
		proposals:       make(map[uint64]*Proposal),
		votes:           make(map[string]map[uint64]bool),
	}
}

// Propose creates a new governance proposal.
func (d *DAOContract) Propose(proposer, title, description string, durationSecs int64) (uint64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nextID++
	p := &Proposal{
		ID:          d.nextID,
		Title:       title,
		Description: description,
		Proposer:    proposer,
		CreatedAt:   time.Now().Unix(),
		DeadlineAt:  time.Now().Unix() + durationSecs,
	}
	d.proposals[p.ID] = p
	return p.ID, nil
}

// Vote casts a vote on a proposal. Weight must not overflow existing vote totals.
func (d *DAOContract) Vote(voter string, proposalID uint64, support bool, weight uint64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.proposals[proposalID]
	if !ok {
		return errors.New("proposal not found")
	}
	if time.Now().Unix() > p.DeadlineAt {
		return errors.New("voting period ended")
	}
	if d.votes[voter] == nil {
		d.votes[voter] = make(map[uint64]bool)
	}
	if d.votes[voter][proposalID] {
		return errors.New("already voted")
	}
	d.votes[voter][proposalID] = true

	// Guard against uint64 overflow
	if support {
		if p.VotesFor+weight < p.VotesFor {
			return errors.New("vote weight overflow")
		}
		p.VotesFor += weight
	} else {
		if p.VotesAgainst+weight < p.VotesAgainst {
			return errors.New("vote weight overflow")
		}
		p.VotesAgainst += weight
	}
	return nil
}

// Execute executes a passed proposal that has met quorum.
func (d *DAOContract) Execute(proposalID uint64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.proposals[proposalID]
	if !ok {
		return errors.New("proposal not found")
	}
	if p.Executed {
		return errors.New("already executed")
	}
	if time.Now().Unix() <= p.DeadlineAt {
		return errors.New("voting still active")
	}
	totalVotes := p.VotesFor + p.VotesAgainst
	if totalVotes < d.QuorumThreshold {
		return errors.New("quorum not reached")
	}
	if p.VotesFor <= p.VotesAgainst {
		return errors.New("proposal did not pass")
	}
	p.Executed = true
	return nil
}

// GetProposal returns a proposal by ID.
func (d *DAOContract) GetProposal(id uint64) (*Proposal, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	p, ok := d.proposals[id]
	if !ok {
		return nil, errors.New("proposal not found")
	}
	return p, nil
}

// --- VM Registry ---

// ContractIface is the common interface for all deployed contracts.
type ContractIface interface {
	contractAddress() string
}

// VM manages all deployed contracts.
type VM struct {
	mu        sync.RWMutex
	contracts map[string]interface{}
}

// NewVM creates a new contract VM.
func NewVM() *VM {
	return &VM{contracts: make(map[string]interface{})}
}

// DeployQRC20 deploys a QRC20 token contract.
func (v *VM) DeployQRC20(owner, name, symbol string, decimals uint8, supply uint64) *QRC20 {
	t := NewQRC20(owner, name, symbol, decimals, supply)
	v.mu.Lock()
	v.contracts[t.Address] = t
	v.mu.Unlock()
	return t
}

// DeployNFT deploys an NFT contract.
func (v *VM) DeployNFT(owner, name, symbol string) *NFTContract {
	c := NewNFTContract(owner, name, symbol)
	v.mu.Lock()
	v.contracts[c.Address] = c
	v.mu.Unlock()
	return c
}

// DeployDAO deploys a DAO contract.
func (v *VM) DeployDAO(owner, name string) *DAOContract {
	d := NewDAOContract(owner, name)
	v.mu.Lock()
	v.contracts[d.Address] = d
	v.mu.Unlock()
	return d
}

// Get returns a contract by address.
func (v *VM) Get(address string) (interface{}, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	c, ok := v.contracts[address]
	if !ok {
		return nil, errors.New("contract not found")
	}
	return c, nil
}

// ContractCount returns the number of deployed contracts.
func (v *VM) ContractCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.contracts)
}

// --- helpers ---

func uint64ToBytes(n uint64) []byte {
	b := make([]byte, 8)
	b[0] = byte(n >> 56)
	b[1] = byte(n >> 48)
	b[2] = byte(n >> 40)
	b[3] = byte(n >> 32)
	b[4] = byte(n >> 24)
	b[5] = byte(n >> 16)
	b[6] = byte(n >> 8)
	b[7] = byte(n)
	return b
}

func bytesToUint64(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}
