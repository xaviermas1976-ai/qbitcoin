# qBitcoin: A Post-Quantum Blockchain Protocol

**Version 0.1 — April 2026**

---

## Abstract

We present qBitcoin, a Layer-1 blockchain protocol designed to be secure against both classical and quantum adversaries. Existing blockchains — including Bitcoin and Ethereum — rely on elliptic-curve cryptography (ECDSA, ECDH) that is known to be vulnerable to Shor's algorithm running on a sufficiently powerful quantum computer. qBitcoin replaces these primitives with NIST-standardized post-quantum algorithms: CRYSTALS-Kyber for key encapsulation and CRYSTALS-Dilithium for digital signatures, combined with a Proof-of-Stake consensus mechanism and a programmable smart contract layer. The result is a blockchain that preserves all properties of existing L1 networks while providing long-term security guarantees in a post-quantum world.

---

## 1. Introduction

On August 13, 2024, the US National Institute of Standards and Technology (NIST) published the first post-quantum cryptography standards: FIPS 203 (CRYSTALS-Kyber, for key encapsulation) and FIPS 204 (CRYSTALS-Dilithium, for digital signatures). These standards represent the culmination of an eight-year standardization process involving global cryptographic experts. Their publication signals that the quantum threat is no longer theoretical — it is a planning horizon.

Blockchain security depends entirely on two cryptographic primitives:

1. **Digital signatures** — proving ownership of funds
2. **Hash functions** — ensuring data integrity

Bitcoin and Ethereum use ECDSA over the secp256k1 curve for signatures. Shor's algorithm, running on a quantum computer with ~4,000 logical qubits, can extract the private key from a known public key in hours. IBM's quantum roadmap targets fault-tolerant systems in this range by 2029.

This creates an urgent problem: **every wallet address that has ever sent a transaction has its public key exposed on-chain**, making it vulnerable to retroactive quantum attack. Migration of existing blockchains to post-quantum cryptography is extremely difficult — it requires consensus from the entire network, migration of all existing wallets, and replacement of deep protocol primitives.

qBitcoin solves this by starting fresh with post-quantum primitives as first-class citizens, not retrofitted patches.

---

## 2. Cryptographic Foundations

### 2.1 CRYSTALS-Kyber (Key Encapsulation)

CRYSTALS-Kyber (FIPS 203) is a key encapsulation mechanism (KEM) based on the hardness of the Module Learning With Errors (MLWE) problem. It provides:

- **Security level:** Kyber-768 targets NIST security level 3 (~192-bit classical, quantum-resistant)
- **Public key size:** 1,184 bytes
- **Ciphertext size:** 1,088 bytes
- **Shared secret:** 32 bytes

In qBitcoin, Kyber is used to generate wallet key pairs and to establish shared secrets for encrypted P2P channels.

### 2.2 CRYSTALS-Dilithium (Digital Signatures)

CRYSTALS-Dilithium (FIPS 204) is a signature scheme based on the hardness of Module Learning With Errors and Module Short Integer Solution (MSIS). It provides:

- **Security level:** Dilithium3 targets NIST security level 3
- **Public key size:** 1,952 bytes
- **Signature size:** 3,293 bytes
- **Verification:** fast, deterministic

Every transaction in qBitcoin is signed with Dilithium3. The signature is verified before a transaction enters the mempool and before a block is accepted.

### 2.3 BLAKE3 Hash Function

qBitcoin uses BLAKE3 for all hashing operations: block hashes, transaction IDs, state root computation, and address derivation. BLAKE3 provides:

- 256-bit output (doubled capacity over SHA-256)
- Tree-hashing structure enabling parallelism
- Resistance to length-extension attacks
- ~3x faster than SHA-256 on modern hardware

### 2.4 Address Derivation

A qBitcoin address is derived as:

```
address = "qBTC" || BLAKE3(BLAKE3(public_key))[:20]
```

This double-hash structure provides an additional security layer: even if BLAKE3 were weakened, the inner hash prevents direct reconstruction.

---

## 3. Proof-of-Stake Consensus

### 3.1 Validator Selection

qBitcoin uses weighted random Proof-of-Stake for block production. Validators lock a minimum stake of 0.01 qBTC to participate. The block producer for each slot is selected via:

```
target = CSPRNG() mod total_stake
```

Where CSPRNG is a cryptographically secure random number generator (OS-level entropy). Validators are sorted deterministically by address before selection to prevent map-iteration bias. This ensures that all nodes in the network, given the same entropy source, select the same validator — providing consensus.

### 3.2 Economic Security

The economic security of the network is proportional to the total staked value. An attacker needs to control >50% of staked tokens to consistently produce fraudulent blocks — a significantly higher bar than Bitcoin's 51% hashrate attack, since acquiring stake requires purchasing tokens on the open market and driving up the price.

### 3.3 Slashing (Roadmap)

A future version will implement slashing: validators who sign conflicting blocks at the same height lose a portion of their stake. This makes equivocation attacks economically irrational.

---

## 4. Transaction Model

### 4.1 Account Model

qBitcoin uses an account model (similar to Ethereum) rather than UTXO. Each address has:
- **Balance** — spendable qBTC
- **Nonce** — monotonically increasing counter preventing replay attacks
- **Stake** — locked tokens for validation

### 4.2 Transaction Types

| Type | Description |
|---|---|
| TRANSFER | Send qBTC between addresses |
| STAKE | Lock tokens as validator stake |
| UNSTAKE | Withdraw staked tokens |
| CONTRACT | Deploy or call a smart contract |
| REWARD | Block reward (internal only) |

### 4.3 Transaction Lifecycle

1. User signs transaction with Dilithium private key
2. Transaction submitted to mempool via REST API or P2P
3. Node validates signature, checks balance and nonce
4. Transaction included in next block by selected validator
5. State updated, SSE event broadcast to connected clients

---

## 5. Smart Contracts

qBitcoin includes a built-in contract VM with three native contract templates:

### 5.1 QRC20 — Fungible Tokens

Equivalent to Ethereum's ERC-20. Supports:
- `Transfer(from, to, amount)`
- `Approve(owner, spender, amount)`
- `TransferFrom(spender, from, to, amount)`
- `BalanceOf(address)`, `TotalSupply()`

All state transitions are atomic and mutex-protected.

### 5.2 NFT — Non-Fungible Tokens

Equivalent to ERC-721. Supports:
- `Mint(to, uri, metadata)`
- `Transfer(from, to, tokenID)`
- `OwnerOf(tokenID)`

Token metadata is stored on-chain. URIs can point to IPFS for large assets.

### 5.3 DAO — Governance

On-chain governance for protocol upgrades and treasury management:
- `Propose(title, description, duration)`
- `Vote(proposalID, support, weight)`
- `Execute(proposalID)` — requires quorum and majority

---

## 6. Network Protocol

### 6.1 P2P Layer

Nodes communicate over TCP with a JSON message protocol. Message types:

| Message | Purpose |
|---|---|
| HANDSHAKE | Node identification |
| NEW_BLOCK | Broadcast minted block |
| NEW_TX | Broadcast transaction |
| GET_PEERS | Request peer list |
| PING/PONG | Liveness check |

Incoming messages are limited to 10 MiB to prevent memory exhaustion. Message deduplication via a 4,096-entry LRU hash cache prevents broadcast storms.

### 6.2 REST API + SSE

The node exposes a REST API on `127.0.0.1:8080` by default. Real-time events are pushed via Server-Sent Events on `/api/v1/events`, enabling lightweight browser and mobile clients without WebSocket complexity.

---

## 7. Tokenomics

| Parameter | Value | Rationale |
|---|---|---|
| Max Supply | 21,000,000 qBTC | Scarcity, follows Bitcoin's proven model |
| Genesis Block | 21,000,000 qBTC | Pre-minted to genesis address |
| Block Reward | 50 qBTC | Validator incentive |
| Block Time | 5 seconds | ~17,280 blocks/day |
| Min Stake | 0.01 qBTC | Low barrier to validator entry |
| Transaction Fee | Market-driven | Prevents spam |

The genesis supply is held in reserve for testnet distribution, ecosystem development, and the founding team (subject to vesting). Unlike Bitcoin's mining-only distribution, qBitcoin's PoS model allows broader participation from day one.

---

## 8. Security Analysis

### 8.1 Quantum Threat Model

| Attack | Classical defense | qBitcoin defense |
|---|---|---|
| Private key from public key | ECDSA (broken by Shor) | Dilithium (MLWE-hard) |
| Key exchange interception | ECDH (broken by Shor) | Kyber (MLWE-hard) |
| Hash preimage | SHA-256 (weakened by Grover) | BLAKE3-256 (128-bit quantum security) |
| 51% stake attack | Economic cost | Same, plus slashing (roadmap) |

### 8.2 Known Limitations (Current Version)

The current release (v0.1) uses a simulation layer for Kyber and Dilithium pending integration of the `circl` library. The simulation is internally consistent and functionally correct for testnet use, but does not provide actual post-quantum security guarantees. **This will be replaced before mainnet.**

---

## 9. Comparison with Existing Projects

| Project | Consensus | PQ Signatures | PQ KEM | Status |
|---|---|---|---|---|
| Bitcoin | PoW | No (ECDSA) | No | Mainnet |
| Ethereum | PoS | No (ECDSA) | No | Mainnet |
| QRL | PoW | XMSS | No | Mainnet |
| IOTA | DAG | Winternitz | No | Mainnet |
| **qBitcoin** | **PoS** | **Dilithium3** | **Kyber-768** | **Testnet** |

qBitcoin is the only PoS blockchain with both NIST-standardized signature and KEM primitives.

---

## 10. Roadmap

### Phase 1 — Testnet Alpha (Q2 2026)
- Post-quantum simulation layer
- PoS consensus, QRC20/NFT/DAO
- REST API, SSE, interactive CLI
- 28-test suite

### Phase 2 — Testnet Beta (Q3 2026)
- Real CRYSTALS-Kyber + Dilithium (`circl`)
- LevelDB persistence
- Public bootstrap nodes
- Block explorer
- Wallet browser extension

### Phase 3 — Security & Audit (Q4 2026)
- Independent cryptographic audit
- Formal verification of consensus logic
- Performance benchmarking

### Phase 4 — Mainnet (Q1 2027)
- Genesis event
- DEX listing
- Cross-chain bridge (Ethereum L2)
- Mobile wallet

---

## 11. Conclusion

The quantum threat to blockchain is not hypothetical — it is a known, scheduled vulnerability with a planning horizon of 3-5 years. qBitcoin provides a clean-slate solution: a modern Proof-of-Stake blockchain built on NIST-standardized post-quantum cryptography, designed to be the secure foundation of digital value in the post-quantum era.

The codebase is open source, written in idiomatic Go, and designed for auditability. We invite cryptographers, developers, and node operators to participate in the testnet and help build the quantum-safe financial infrastructure the world will need.

---

## References

1. NIST FIPS 203 — Module-Lattice-Based Key-Encapsulation Mechanism Standard (2024)
2. NIST FIPS 204 — Module-Lattice-Based Digital Signature Standard (2024)
3. Nakamoto, S. — Bitcoin: A Peer-to-Peer Electronic Cash System (2008)
4. Shor, P. — Polynomial-Time Algorithms for Prime Factorization (1994)
5. Aumasson, J.P. et al. — BLAKE3 (2020)
6. IBM Quantum Roadmap — ibm.com/quantum/roadmap (2024)

---

*qBitcoin v0.1 — Open source, MIT License*
*github.com/yourusername/qbitcoin*
