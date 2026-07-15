# Technical Specification: Yona Hash Algorithm (Yona Hash)

**Yona Hash** is a cryptographic hashing algorithm specifically optimized for the YonaCode network. The algorithm inherits the ultra-fast Merkle tree structure of Blake3 but comprehensively modifies the core mixing function $G$ (Mix Function) to a new shift constant system and inserts an identifying noise key $Y_{key}$ to completely invalidate existing commercial ASIC miners, and we do not intend to fight against ASICs.

---

## 1. Constants
*   **Magic Key:** $Y_{key} = \text{0x594F4E41}$ (Representing the ASCII string "YONA").
*   **New Rotation Shifts:** $R = \{17, 13, 9, 5\}$ (Replacing the traditional $\{16, 12, 8, 7\}$ set).

---

## 2. Core Compression Function (The G Function)
Given $A, B, C, D$ as 32-bit state words, and $X, Y$ as input message words:

$$A = A + B + (X \oplus Y_{key}) \pmod{2^{32}}$$

$$D = (D \oplus A) \ggg 17$$

$$C = C + D \pmod{2^{32}}$$

$$B = (B \oplus C) \ggg 13$$

$$A = A + B + (Y \oplus Y_{key}) \pmod{2^{32}}$$

$$D = (D \oplus A) \ggg 9$$

$$C = C + D \pmod{2^{32}}$$

$$B = (B \oplus C) \ggg 5$$

*(Where $\oplus$ is the bitwise XOR operation, $+$ is addition modulo $2^{32}$, and $\ggg$ is the bitwise right rotation).*

---

## 3. Compression Architecture
The compression function of Yona Hash takes a chaining value state `[u32; 8]`, input data block `[u32; 16]`, block counter `counter` (u64), block length `block_len` (u32), and control flags `flags` (u32) to initialize a 16-word state matrix `[u32; 16]`.

The algorithm executes 7 mixing rounds. Each round applies the $G$ transformation on the columns and diagonals of the state matrix, followed by message word permutation using Blake3's standard permutation table. The final 512-bit output (16 u32 words) is XORed between the ending state and the initial chaining values to ensure error propagation and security.
