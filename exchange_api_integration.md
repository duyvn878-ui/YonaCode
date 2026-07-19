# 🏦 YonaCode Exchange API & EBP Integration Guide

This document provides detailed technical guidance for Crypto Exchange Systems Engineers and Developers to integrate deposit/withdrawal flows, hot wallet management, and utilize the **Exchange Batch Protocol (EBP - `TXSQ`)** for ultra-high throughput processing on the YonaCode Blockchain network.

---

## 📑 Table of Contents
1. [Overview of EBP (Exchange Batch Protocol)](#1-overview-of-ebp-exchange-batch-protocol)
2. [Binary Structure of `TXSQ` Batch](#2-binary-structure-of-txsq-batch)
3. [Hot Wallet & Exchange Nonce Management Workflow](#3-hot-wallet--exchange-nonce-management-workflow)
4. [RPC API Reference for Exchanges](#4-rpc-api-reference-for-exchanges)
5. [Integration Code Examples (Go / Python / Node.js)](#5-integration-code-examples-go--python--nodejs)
6. [Security Standards & Hot Wallet Operational Principles](#6-security-standards--hot-wallet-operational-principles)

---

## 1. Overview of EBP (Exchange Batch Protocol)

During mass withdrawal events, submitting individual transactions sequentially risks network congestion and nonce desynchronization. YonaCode Blockchain introduces the **EBP (`TXSQ`)** standard, allowing Exchanges to:
* **Batch hundreds of withdrawal transactions into a single binary payload.**
* **Ensure strict sequential nonce ordering:** Withdrawal transactions are ordered with strictly increasing nonces (`N`, `N+1`, `N+2`...) directly at emission.
* **Ultra-fast processing via Mempool Bus Stream (2-Second Bus):** Nodes bypass redundant checks, decompress, and feed transaction batches directly into the Rust Core for atomic state finalization.

---

## 2. Binary Structure of `TXSQ` Batch

An EBP transaction package sent over the P2P network or RPC API endpoint must be encoded in the following binary layout:

| Field | Size | Data Type | Description |
| :--- | :--- | :--- | :--- |
| **Magic Header** | 4 Bytes | ASCII String | Fixed value `"TXSQ"` (`0x54 0x58 0x53 0x51`) |
| **Exchange Address** | 32 Bytes | Raw Bytes | Exchange Hot Wallet Address |
| **Batch ID / SeqNum** | 8 Bytes | Big-Endian uint64 | Batch Identifier / Sequence Number |
| **Start Nonce** | 8 Bytes | Big-Endian uint64 | Nonce of the first transaction in the batch |
| **End Nonce** | 8 Bytes | Big-Endian uint64 | Nonce of the last transaction in the batch |
| **Tx Count** | 4 Bytes | Big-Endian uint32 | Total number of transactions in batch (max 200 TX/batch) |
| **Payload Data** | Variable | Length-prefixed | Array of length-prefixed raw transaction byte arrays |

---

## 3. Hot Wallet & Exchange Nonce Management Workflow

To prevent Nonce mismatch errors (Code 105 / Code 106) during high-volume withdrawals, Exchanges must adhere to a 3-step workflow:

```
[Step 1: Query Expected Nonce] ➡️ [Step 2: Sign Transaction Sequence] ➡️ [Step 3: Broadcast EBP Batch]
     (GetExpectedNonce)               (Nonce N, N+1, N+2...)             (Broadcast TXSQ)
```

1. **Step 1:** Call RPC `GetExpectedNonce(hot_wallet_address)` to retrieve the target signing nonce. This method automatically calculates pending nonces in the RAM Mempool.
2. **Step 2:** Assign strictly incremental nonces across the withdrawal queue:
   * Withdrawal 1: `Nonce = N`
   * Withdrawal 3: `Nonce = N + 1`
   * Withdrawal 3: `Nonce = N + 2`
3. **Step 3:** Pack the sequence into `TXSQ` binary format and submit it to the Node RPC API.

---

## 4. RPC API Reference for Exchanges

YonaCode Node exposes REST/JSON-RPC (default port `9090`) and gRPC (default port `18080`) interfaces.

### 4.1. Query Hot Wallet Balance & Nonce
* **REST Endpoint:** `GET /api/v1/account/{address}`
* **gRPC Method:** `SclService/GetNonce` & `SclService/GetAccountState`
* **Response JSON Example:**
```json
{
  "address": "0x680303fe459c4622e35c279347755db9b1139776fab81f83d8eaa141fa080146",
  "balance": "1000000000000",
  "nonce": 34,
  "expected_nonce": 34
}
```

### 4.2. Broadcast EBP Withdrawal Batch (Bulk Withdrawal)
* **REST Endpoint:** `POST /api/v1/ebp/broadcast`
* **Body:** Binary `TXSQ` payload or JSON array of raw signed transactions.
* **Response JSON Example:**
```json
{
  "success": true,
  "batch_id": 1052,
  "processed_count": 50,
  "start_nonce": 34,
  "end_nonce": 83,
  "status": "ACCEPTED_MEMPOOL_BUS"
}
```

### 4.3. Subscribe to Deposit Events (Deposit Webhook / SSE)
* **SSE Endpoint:** `GET /api/v1/events/stream`
* **Description:** Real-time event streaming for incoming deposit transactions targeting Exchange addresses as soon as blocks achieve atomic finality (Nuclear Shield State Finality).

---

## 5. Integration Code Examples (Go / Python / Node.js)

### 5.1. Packing `TXSQ` Batch in Go (Golang)

```go
package main

import (
	"encoding/binary"
)

// PackSequentialBatch encodes an EBP batch matching exchange specifications
func PackSequentialBatch(exchangeAddr []byte, batchId uint64, startNonce uint64, endNonce uint64, txsBytes [][]byte) []byte {
	var buf []byte

	// 1. Magic Header "TXSQ"
	buf = append(buf, []byte("TXSQ")...)

	// 2. Exchange Address (32 bytes)
	addrBytes := make([]byte, 32)
	copy(addrBytes, exchangeAddr)
	buf = append(buf, addrBytes...)

	// 3. Batch ID (8 bytes)
	bId := make([]byte, 8)
	binary.BigEndian.PutUint64(bId, batchId)
	buf = append(buf, bId...)

	// 4. Start Nonce (8 bytes)
	sNonce := make([]byte, 8)
	binary.BigEndian.PutUint64(sNonce, startNonce)
	buf = append(buf, sNonce...)

	// 5. End Nonce (8 bytes)
	eNonce := make([]byte, 8)
	binary.BigEndian.PutUint64(eNonce, endNonce)
	buf = append(buf, eNonce...)

	// 6. Transaction Count (4 bytes)
	count := make([]byte, 4)
	binary.BigEndian.PutUint32(count, uint32(len(txsBytes)))
	buf = append(buf, count...)

	// 7. Append length-prefixed raw transactions
	for _, tx := range txsBytes {
		txLen := make([]byte, 4)
		binary.BigEndian.PutUint32(txLen, uint32(len(tx)))
		buf = append(buf, txLen...)
		buf = append(buf, tx...)
	}

	return buf
}
```

### 5.2. Packing `TXSQ` Batch in Python

```python
import struct

def pack_sequential_batch(exchange_addr_bytes, batch_id, start_nonce, end_nonce, txs_bytes_list):
    buf = bytearray()
    
    # 1. Magic Header "TXSQ"
    buf.extend(b"TXSQ")
    
    # 2. Address (32 bytes)
    addr_padded = exchange_addr_bytes.ljust(32, b'\x00')[:32]
    buf.extend(addr_padded)
    
    # 3. Batch ID, Start Nonce, End Nonce (Big-Endian uint64)
    buf.extend(struct.pack(">Q", batch_id))
    buf.extend(struct.pack(">Q", start_nonce))
    buf.extend(struct.pack(">Q", end_nonce))
    
    # 4. Tx Count (Big-Endian uint32)
    buf.extend(struct.pack(">I", len(txs_bytes_list)))
    
    # 5. Raw Transactions
    for tx in txs_bytes_list:
        buf.extend(struct.pack(">I", len(tx)))
        buf.extend(tx)
        
    return bytes(buf)
```

---

## 6. Security Standards & Hot Wallet Operational Principles

1. **Batch Chunking Limits:** Each `TXSQ` batch should be chunked to a maximum of **200 transactions per batch** to optimize utilization of the Node's 2-second bus stream pipeline.
2. **Block Confirmation Height:** For incoming Deposits, it is recommended that Exchanges wait for at least **3 Block Confirmations** before crediting user balances (even though YonaCode features atomic anti-reorg mechanics).
3. **Account Creation Fee:** When executing withdrawals to a completely new address (with no prior state history on the ledger), the system levies a state initialization fee of `1000 nanoVNT`. Exchanges should inspect target address history to accurately estimate fees.
