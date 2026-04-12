package wallet

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"qbitcoin/internal/crypto"
)

// Wallet represents a post-quantum qBitcoin wallet.
type Wallet struct {
	Address    string `json:"address"`
	PublicKey  []byte `json:"public_key"`
	privateKey []byte
	CreatedAt  int64  `json:"created_at"`
	Label      string `json:"label,omitempty"`
}

// NewWallet creates a new post-quantum wallet.
func NewWallet(label string) (*Wallet, error) {
	kp, err := crypto.GenerateKyberKeyPair()
	if err != nil {
		return nil, err
	}
	addr := crypto.AddressFromPublicKey(kp.PublicKey)
	return &Wallet{
		Address:    addr,
		PublicKey:  kp.PublicKey,
		privateKey: kp.PrivateKey,
		CreatedAt:  time.Now().UnixNano(),
		Label:      label,
	}, nil
}

// Sign signs data with the wallet's private key.
func (w *Wallet) Sign(data []byte) ([]byte, error) {
	if w.privateKey == nil {
		return nil, errors.New("private key not available (read-only wallet)")
	}
	return crypto.Sign(w.privateKey, data)
}

// Verify verifies a signature against this wallet's public key.
func (w *Wallet) Verify(data, signature []byte) bool {
	return crypto.Verify(w.PublicKey, data, signature)
}

// WalletInfo is the public-safe wallet representation.
type WalletInfo struct {
	Address   string `json:"address"`
	PublicKey string `json:"public_key"`
	CreatedAt int64  `json:"created_at"`
	Label     string `json:"label,omitempty"`
}

// ExportPublic returns the wallet's public info without the private key.
func (w *Wallet) ExportPublic() WalletInfo {
	return WalletInfo{
		Address:   w.Address,
		PublicKey: hex.EncodeToString(w.PublicKey),
		CreatedAt: w.CreatedAt,
		Label:     w.Label,
	}
}

// walletFile is the on-disk format.
// TODO: encrypt PrivateKey with Argon2id+AES-256-GCM before production use.
type walletFile struct {
	Address    string `json:"address"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	CreatedAt  int64  `json:"created_at"`
	Label      string `json:"label,omitempty"`
}

// Save saves the wallet to a file.
// WARNING: private key is stored as hex. Encrypt before production use.
func (w *Wallet) Save(path string) error {
	wf := walletFile{
		Address:    w.Address,
		PublicKey:  hex.EncodeToString(w.PublicKey),
		PrivateKey: hex.EncodeToString(w.privateKey),
		CreatedAt:  w.CreatedAt,
		Label:      w.Label,
	}
	data, err := json.MarshalIndent(wf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Load loads a wallet from a file, verifying address integrity.
func Load(path string) (*Wallet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wf walletFile
	if err := json.Unmarshal(data, &wf); err != nil {
		return nil, err
	}
	pub, err := hex.DecodeString(wf.PublicKey)
	if err != nil {
		return nil, err
	}
	priv, err := hex.DecodeString(wf.PrivateKey)
	if err != nil {
		return nil, err
	}

	// Verify address integrity
	expectedAddr := crypto.AddressFromPublicKey(pub)
	if expectedAddr != wf.Address {
		return nil, errors.New("wallet file integrity check failed: address mismatch")
	}

	return &Wallet{
		Address:    wf.Address,
		PublicKey:  pub,
		privateKey: priv,
		CreatedAt:  wf.CreatedAt,
		Label:      wf.Label,
	}, nil
}

// Manager manages multiple wallets.
type Manager struct {
	mu      sync.RWMutex
	wallets map[string]*Wallet
}

// NewManager creates a new wallet manager.
func NewManager() *Manager {
	return &Manager{wallets: make(map[string]*Wallet)}
}

// Create creates and stores a new wallet.
func (m *Manager) Create(label string) (*Wallet, error) {
	w, err := NewWallet(label)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wallets[w.Address] = w
	return w, nil
}

// Get returns a wallet by address.
func (m *Manager) Get(address string) (*Wallet, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.wallets[address]
	if !ok {
		return nil, errors.New("wallet not found")
	}
	return w, nil
}

// List returns all wallet addresses.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	addrs := make([]string, 0, len(m.wallets))
	for addr := range m.wallets {
		addrs = append(addrs, addr)
	}
	return addrs
}

// Import imports an existing wallet.
func (m *Manager) Import(w *Wallet) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wallets[w.Address] = w
}

// Delete removes a wallet from the manager and zeros its private key in memory.
func (m *Manager) Delete(address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.wallets[address]
	if !ok {
		return errors.New("wallet not found")
	}
	// Zero private key bytes before releasing
	for i := range w.privateKey {
		w.privateKey[i] = 0
	}
	delete(m.wallets, address)
	return nil
}
