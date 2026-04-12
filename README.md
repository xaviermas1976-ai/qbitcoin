# qBitcoin ⚛

> The first blockchain designed from the ground up for the post-quantum era.

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Status](https://img.shields.io/badge/Status-Testnet-orange)](https://github.com/yourusername/qbitcoin)

---

## The Problem

Quantum computers will break Bitcoin, Ethereum, and every existing blockchain.

Shor's algorithm running on a sufficiently powerful quantum computer can derive private keys from public keys in polynomial time — making every wallet on every existing blockchain vulnerable. The US National Institute of Standards and Technology (NIST) finalized its first post-quantum cryptography standards in 2024 (FIPS 203, FIPS 204). No major blockchain has integrated them yet.

**qBitcoin is built on those standards from day one.**

---

## What is qBitcoin?

qBitcoin is a Layer-1 blockchain written in Go that replaces classical elliptic-curve cryptography with NIST-standardized post-quantum primitives:

| Classical (vulnerable) | qBitcoin |
|---|---|
| ECDSA signatures | CRYSTALS-Dilithium (FIPS 204) |
| ECDH key exchange | CRYSTALS-Kyber (FIPS 203) |
| SHA-256 | BLAKE3 |
| Proof of Work | Proof of Stake (energy efficient) |

---

## Key Features

- **Post-Quantum Wallets** — addresses derived from Kyber-768 key pairs
- **Quantum-Resistant Transactions** — signed with Dilithium3
- **Proof of Stake Consensus** — energy-efficient, cryptographically secure validator selection
- **Smart Contracts** — QRC20 tokens, NFTs, and DAO governance
- **Real-Time API** — REST + Server-Sent Events
- **Pure Go** — no CGO, cross-platform, single binary

---

## Architecture

```
qbitcoin/
├── cmd/
│   ├── main.go          # Full node (PoS + P2P + API)
│   └── cli/main.go      # Interactive CLI
└── internal/
    ├── crypto/          # Kyber KEM + Dilithium signatures + BLAKE3
    ├── blockchain/      # Blocks, transactions, PoS consensus, state
    ├── wallet/          # Post-quantum wallet management
    ├── p2p/             # TCP peer-to-peer network
    ├── contracts/       # QRC20, NFT, DAO smart contracts
    └── api/             # REST API + SSE real-time events
```

---

## Quick Start

**Requirements:** Go 1.21+

```bash
git clone https://github.com/yourusername/qbitcoin
cd qbitcoin
go mod tidy

# Terminal 1 — Start the node
go run ./cmd/main.go

# Terminal 2 — Interactive CLI
go run ./cmd/cli/main.go
```

### CLI Commands

```
status                          Node status (height, mempool, peers)
blocks [limit] [offset]         List blocks
block <index>                   Block details
wallet new [label]              Create post-quantum wallet
wallet <address>                Balance and info
send <from> <to> <amount>       Send transaction
validator register <addr> <stake>  Register as validator
mempool                         Pending transactions
help                            All commands
```

### REST API

```bash
GET  /api/v1/status
GET  /api/v1/blocks?limit=10&offset=0
GET  /api/v1/block/{index}
POST /api/v1/tx
POST /api/v1/wallet/new
GET  /api/v1/wallet/{address}
POST /api/v1/validator/register
POST /api/v1/contract/qrc20
POST /api/v1/contract/nft
POST /api/v1/contract/dao
GET  /api/v1/events?channel=blocks   (SSE)
```

---

## Node Flags

```bash
go run ./cmd/main.go \
  -api 127.0.0.1:8080 \
  -p2p :9090 \
  -peers "192.168.1.10:9090,192.168.1.11:9090" \
  -data ./data
```

---

## Run Tests

```bash
go test ./... -v
```

28 tests covering crypto, blockchain, wallets, contracts.

---

## Tokenomics

| Parameter | Value |
|---|---|
| Max Supply | 21,000,000 qBTC |
| Block Reward | 50 qBTC |
| Block Time | 5 seconds |
| Min Validator Stake | 0.01 qBTC |
| Consensus | Proof of Stake |

---

## Roadmap

### Phase 1 — Testnet (now)
- [x] Post-quantum key pairs
- [x] PoS consensus
- [x] QRC20 / NFT / DAO contracts
- [x] REST API + SSE
- [x] Interactive CLI

### Phase 2 — Q3 2026
- [ ] Real CRYSTALS-Kyber + Dilithium (circl library)
- [ ] Persistent storage (LevelDB)
- [ ] Public testnet with bootstrap nodes
- [ ] Block explorer

### Phase 3 — Q4 2026
- [ ] Wallet browser extension
- [ ] Cross-chain bridge (Ethereum L2)
- [ ] Independent security audit
- [ ] Mainnet launch

---

## Why Now?

IBM's quantum roadmap targets fault-tolerant systems by 2029. Migration of legacy blockchains will take years. The window to build quantum-safe infrastructure is **now**, before the threat materializes.

> "Store now, decrypt later" attacks are already happening — adversaries collect encrypted data today to decrypt once quantum computers are available.

---

## Contributing

Pull requests welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

Areas where help is most needed:
- Integration of real `circl` Kyber/Dilithium
- LevelDB persistence layer
- Browser wallet (WebAssembly)
- P2P peer discovery (Kademlia DHT)

---

## License

MIT — free to use, modify, and distribute.

---

## Contact

Built with Go. Inspired by the post-quantum future.

*"The best time to build quantum-resistant infrastructure was 10 years ago. The second best time is now."*
