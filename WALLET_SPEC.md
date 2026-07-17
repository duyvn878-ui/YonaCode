# WALLET DERIVATION AND RESTORATION STANDARD (YONACODE PROTOCOL)

This document defines the standardized wallet restoration formula for the entire YonaCode ecosystem. Any developer building third-party wallet applications (CLI, Mobile Wallet, Web Extensions, Web App) **must strictly adhere** to this specification. This prevents address mismatches during restoration, which can cause severe user panic by creating the illusion of lost assets.

---

## 1. BACKGROUND AND OBJECTIVES

In blockchain ecosystems, it is common to have multiple wallet implementations. However, if developers apply ad-hoc preprocessing to the mnemonic phrase (such as trimming whitespace, converting to lowercase, or coalescing spaces) or use incorrect derivation paths (e.g., BIP44 path with indices), the same 12-word mnemonic phrase will generate different addresses across different wallet clients.

This creates a false **"lost wallet/lost coins" illusion**, which severely damages system trust and creates operational risks. To resolve this, YonaCode establishes the **Go-Rust Core as the Single Source of Truth**. All wallets must strictly match the exact key derivation logic of the Core client.

---

## 2. CANONICAL DERIVATION FORMULA

The system uses a static, single-address-per-mnemonic scheme with no derivation index (no BIP44 path) defined by the following formula:

$$\text{Key} = \text{Ed25519}(\text{SHA256}(\text{PBKDF2}(\text{mnemonic} + \text{passphrase})))$$

### Step-by-Step Specification:

1. **Mnemonic Input:** Receive the raw 12-word BIP-39 mnemonic phrase.
   > [!IMPORTANT]
   > **Invariant Rule:** Do NOT apply any preprocessing or normalization (such as `trim()`, `toLowerCase()`, or whitespace merging) to the mnemonic string before passing it to the BIP39 seed generator. The raw string entered by the user must be passed as-is to guarantee 100% binary parity with the Go-Rust Core node.
   
2. **BIP39 Seed (64 bytes):** Generate a standard BIP39 seed using PBKDF2 with HMAC-SHA512 and 2048 iterations. The salt is defined as `"mnemonic" + passphrase` (where passphrase defaults to an empty string `""` if not provided).
3. **Hashed Seed (32 bytes):** Compute the SHA-256 hash of the 64-byte seed to produce a 32-byte seed for Ed25519.
4. **Ed25519 Keypair:** Instantiate the Ed25519 keypair using the 32-byte hashed seed.
5. **Wallet Address:** The address is the hex representation of the 32-byte Ed25519 Public Key, displayed to the user with a lowercase `0x` prefix (e.g., `0x680303fe...`).

---

## 3. REFERENCE IMPLEMENTATIONS

### 🐹 Go (Node Core)
```go
import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"github.com/tyler-smith/go-bip39"
)

func DeriveWallet(mnemonic string, passphrase string) (string, error) {
	// 1. Generate BIP39 seed from the raw mnemonic
	seed := bip39.NewSeed(mnemonic, passphrase)
	
	// 2. Hash using SHA-256
	hash := sha256.Sum256(seed)
	hashedSeed := hash[:]
	
	// 3. Instantiate Ed25519 key
	priv := ed25519.NewKeyFromSeed(hashedSeed)
	pub := priv.Public().(ed25519.PublicKey)
	
	// Wallet address (64-character hex string)
	addr := fmt.Sprintf("%x", pub)
	return addr, nil
}
```

### ⚛️ TypeScript / JavaScript (Web & Mobile Wallets)
Using `@scure/bip39`, `@noble/hashes` and `tweetnacl`:

```typescript
import { mnemonicToSeedSync } from '@scure/bip39';
import { sha256 } from '@noble/hashes/sha2.js';
import nacl from 'tweetnacl';

export function deriveKeyPair(mnemonic: string, passphrase = '') {
  // 1. Generate standard raw BIP39 seed (64 bytes)
  const seed64 = mnemonicToSeedSync(mnemonic, passphrase);
  
  // 2. Compute SHA-256 using noble hashes (deterministic across HTTP/HTTPS)
  const hashedSeed = sha256(seed64);

  // 3. Generate Ed25519 keypair
  const keyPair = nacl.sign.keyPair.fromSeed(hashedSeed);
  
  // Convert public key to lowercase hex string with 0x prefix
  const address = '0x' + Array.from(keyPair.publicKey)
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');

  return {
    privateKey: keyPair.secretKey, // 64 bytes (contains seed and pubkey at the end)
    publicKey: keyPair.publicKey,   // 32 bytes
    address
  };
}
```

---

## 4. KEY SECURITY GUIDELINES
* **Do not store private keys:** Never send mnemonic phrases or private keys to external servers, and do not log them anywhere.
* **Automated Sync:** All formulas regarding transaction structure and signing must match the Blake3 hashing logic of the Core node.
