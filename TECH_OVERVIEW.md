# YonaCode Project Technology Overview

The project utilizes a hybrid architecture that combines the high-performance computation of Rust with the decentralized networking capabilities of Go. The system is designed to be minimal, focusing on security and hardware resource optimization.

## 1. Architecture & IPC
* **Rust (Ledger Core & State Machine):** Responsible for balance calculations, block validation, account state management, and consensus synchronization algorithms.
* **Go (Network & API Layer):** Manages peer-to-peer (P2P) connections, the pending transaction queue (Mempool), RPC servers, and the Web interface server.
* **gRPC Communication:** The two layers run on independent processes and communicate via the gRPC protocol (using Tonic/Prost on the Rust side and grpc-go on the Go side), supporting message sizes up to 512MB to handle block chunks during synchronization.
* **Local IPC (Inter-Process Communication):** Employs Named Pipes on Windows and Unix Sockets on Unix/Linux as a low-latency alternative to TCP Loopback.

## 2. Storage & State Engine
* **RocksDB:** Key-Value database storage that organizes data into Column Families (such as flat accounts, block headers, and transaction receipts). It is optimized with LRU block cache and write buffers up to 256MB to avoid I/O bottlenecks.
* **Jellyfish Merkle Tree (JMT):** A versioned Sparse Merkle Tree structure. It manages account state storage, provides cryptographic proofs (Merkle Proofs) for the StateRoot, and supports secure state rollbacks to historical blocks without corrupting flat account data.

## 3. P2P Networking
* **libp2p:** A comprehensive peer-to-peer networking stack supporting multiple transport protocols (TCP, UDP/QUIC), UPnP/NAT-PMP port mapping, AutoNAT self-diagnosis of Public/Private status, and Kademlia DHT for node discovery.
* **Gossipsub:** A high-performance pub/sub protocol used to propagate Compact Blocks and pending transactions. It integrates strict validation filters to silently reject spam blocks or invalid transactions at the network layer.
* **Watermill (Event Bus):** An in-memory local PubSub system to decouple processing between Go-side modules: Mempool, SyncEngine, and Network.

## 4. Cryptography & Security
* **Blake3 Hashing:** A high-speed hashing algorithm used for block headers and Merkle trees. It leverages a key derivation mechanism (DeriveKey) combined with custom security context strings to render standard ASIC mining hardware ineffective.
* **Ed25519 Signatures:** The Ed25519 digital signature standard is used to validate transactions. The Rust core integrates a signature verification cache (RwLock<HashMap>) using Blake3 hashes to protect the system against signature bomb DoS attacks.
* **Argon2id & AES-GCM:** Employs the Argon2id key derivation function combined with AES-GCM encryption to secure local wallet private keys in JSON file formats.

## 5. Concurrency & Performance Optimization
* **Rayon (Rust):** A data-parallelism library for multi-core CPUs, utilized in the Rust core for parallel transaction signature verification and proof-of-work (PoW) mining execution.
* **Parallel Prefetching:** Pre-loads relevant account states from RocksDB into RAM cache in parallel prior to executing block transaction pipelines, minimizing disk I/O latency.
* **EBP (Exchange Batch Protocol):** An ordered batch packaging and signing protocol that bundles thousands of transactions into a single payload, reducing network overhead and lock contention.

## 6. Interface & API Communication
* **React (Embedded Web UI):** Wallet interface and dashboard displaying real-time metrics such as hashrate, total supply, block height, and transaction history.
* **Go Embed:** The entire production Web UI build is embedded directly into the Go executable using the `go:embed` feature, enabling a lightweight single-binary deployment.
* **SSE (Server-Sent Events) & JSON-RPC:** Provides low-latency, real-time data streaming of network state updates from the Node directly to the user's browser.
