import { generateMnemonic, mnemonicToSeedSync, validateMnemonic } from '@scure/bip39';
import { wordlist } from '@scure/bip39/wordlists/english.js';
import nacl from 'tweetnacl';
import { blake3 } from '@noble/hashes/blake3.js';
import { sha256 } from '@noble/hashes/sha2.js';

// Định nghĩa kiểu cho giao dịch chưa ký
export interface UnsignedTx {
  version: number;
  sender: string; // Hex string 0x...
  receiver: string; // Hex string 0x...
  amount: number; // VNT
  fee: number; // VNT
  nonce: number;
  timestamp: number;
  recent_block_hash: string; // Hex string 0x...
  chain_id: number;
}

// Định nghĩa kiểu cho giao dịch đã ký
export interface SignedTx extends UnsignedTx {
  signature: string; // Hex string
}

// 1. Sinh Mnemonic ngẫu nhiên (12 từ khóa)
export function generateNewMnemonic(): string {
  return generateMnemonic(wordlist);
}

// 2. Kiểm tra Mnemonic hợp lệ
export function isValidMnemonic(mnemonic: string): boolean {
  return validateMnemonic(mnemonic.trim(), wordlist);
}

// Helper: Chuyển đổi chuỗi Hex thành Uint8Array
export function hexToBytes(hex: string): Uint8Array {
  const cleanHex = hex.replace(/^0x/, '');
  const bytes = new Uint8Array(cleanHex.length / 2);
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(cleanHex.substr(i * 2, 2), 16);
  }
  return bytes;
}

// Helper: Chuyển đổi Uint8Array thành chuỗi Hex
export function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');
}

// 3. Tạo cặp khóa Ed25519 từ Mnemonic + Passphrase
// Công thức: Mnemonic + Passphrase -> BIP39 Seed (64 bytes) -> SHA256 -> 32 bytes Seed -> Ed25519 Keypair
export async function deriveKeyPairFromMnemonic(mnemonic: string, passphrase = ''): Promise<{
  privateKey: Uint8Array;
  publicKey: Uint8Array;
  address: string;
}> {
  // 1. BIP39 Seed (64 bytes)
  const seed64 = mnemonicToSeedSync(mnemonic, passphrase);
  
  // 2. SHA-256 of Seed (32 bytes)
  const hashedSeed = sha256(seed64);

  // 3. Ed25519 keypair
  const keyPair = nacl.sign.keyPair.fromSeed(hashedSeed);
  
  const address = '0x' + bytesToHex(keyPair.publicKey);

  return {
    privateKey: keyPair.secretKey, // 64 bytes (Ed25519 secret key chứa cả public key ở cuối)
    publicKey: keyPair.publicKey, // 32 bytes
    address
  };
}

// 4. Mã hóa Private Key bằng AES-GCM-256 dùng PIN 6 số
export async function encryptPrivateKey(privateKey: Uint8Array, pin: string): Promise<{
  encryptedHex: string;
  saltHex: string;
  ivHex: string;
}> {
  const salt = window.crypto.getRandomValues(new Uint8Array(16));
  const iv = window.crypto.getRandomValues(new Uint8Array(12));

  // Deriving key from PIN
  const pinBytes = new TextEncoder().encode(pin);
  const baseKey = await window.crypto.subtle.importKey(
    'raw',
    pinBytes,
    'PBKDF2',
    false,
    ['deriveKey']
  );

  const aesKey = await window.crypto.subtle.deriveKey(
    {
      name: 'PBKDF2',
      salt: salt as any,
      iterations: 100000,
      hash: 'SHA-256'
    },
    baseKey,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt']
  );

  const encryptedBuffer = await window.crypto.subtle.encrypt(
    {
      name: 'AES-GCM',
      iv: iv as any
    },
    aesKey,
    privateKey as any
  );

  return {
    encryptedHex: bytesToHex(new Uint8Array(encryptedBuffer)),
    saltHex: bytesToHex(salt),
    ivHex: bytesToHex(iv)
  };
}

// 5. Giải mã Private Key bằng PIN 6 số
export async function decryptPrivateKey(encryptedHex: string, saltHex: string, ivHex: string, pin: string): Promise<Uint8Array> {
  const encryptedBytes = hexToBytes(encryptedHex);
  const salt = hexToBytes(saltHex);
  const iv = hexToBytes(ivHex);

  const pinBytes = new TextEncoder().encode(pin);
  const baseKey = await window.crypto.subtle.importKey(
    'raw',
    pinBytes as any,
    'PBKDF2',
    false,
    ['deriveKey']
  );

  const aesKey = await window.crypto.subtle.deriveKey(
    {
      name: 'PBKDF2',
      salt: salt as any,
      iterations: 100000,
      hash: 'SHA-256'
    },
    baseKey,
    { name: 'AES-GCM', length: 256 },
    false,
    ['decrypt']
  );

  try {
    const decryptedBuffer = await window.crypto.subtle.decrypt(
      {
        name: 'AES-GCM',
        iv: iv as any
      },
      aesKey,
      encryptedBytes as any
    );
    return new Uint8Array(decryptedBuffer);
  } catch {
    throw new Error('Sai mã PIN bảo mật!');
  }
}

// Helper: Ghi số uint64 dạng Little Endian vào buffer
function writeUint64LE(value: number): Uint8Array {
  const buf = new ArrayBuffer(8);
  const view = new DataView(buf);
  // JavaScript numbers là double-precision floats, chúng ta xử lý an toàn dưới dạng low/high 32-bit uints
  const low = value % 0x100000000;
  const high = Math.floor(value / 0x100000000);
  view.setUint32(0, low, true);
  view.setUint32(4, high, true);
  return new Uint8Array(buf);
}

// Helper: Ghi số uint32 dạng Little Endian vào buffer
function writeUint32LE(value: number): Uint8Array {
  const buf = new ArrayBuffer(4);
  const view = new DataView(buf);
  view.setUint32(0, value, true);
  return new Uint8Array(buf);
}

// Helper: Chuẩn hóa chuỗi hex về 32 bytes chuẩn Ed25519 (bỏ tiền tố 03 nếu có)
export function hexToRaw32Bytes(hex: string): Uint8Array {
  const cleanBytes = hexToBytes(hex);
  if (cleanBytes.length === 33) {
    return cleanBytes.slice(1);
  }
  if (cleanBytes.length > 32) {
    return cleanBytes.slice(cleanBytes.length - 32);
  }
  return cleanBytes;
}

// 6. Tính Signing Hash của giao dịch thô
// Cấu trúc phân rã nhị phân tương thích 100% với GetSigningHashNative của Go Node
export function getTransactionSigningHash(tx: UnsignedTx): Uint8Array {
  const chunks: Uint8Array[] = [];

  // 1. Version (uint64)
  chunks.push(writeUint64LE(tx.version));

  // 2. Sender (32 bytes chuẩn Ed25519)
  const senderBytes = hexToRaw32Bytes(tx.sender);
  chunks.push(writeUint32LE(senderBytes.length));
  chunks.push(senderBytes);

  // 3. Receiver (32 bytes chuẩn Ed25519)
  const receiverBytes = hexToRaw32Bytes(tx.receiver);
  chunks.push(writeUint32LE(receiverBytes.length));
  chunks.push(receiverBytes);

  // 4. Amount (uint64)
  chunks.push(writeUint64LE(tx.amount));

  // 5. Fee (uint64)
  chunks.push(writeUint64LE(tx.fee));

  // 6. Nonce (uint64)
  chunks.push(writeUint64LE(tx.nonce));

  // 7. Timestamp (uint64)
  chunks.push(writeUint64LE(tx.timestamp));

  // 8. Recent Block Hash
  const recentBlockBytes = hexToBytes(tx.recent_block_hash);
  chunks.push(writeUint32LE(recentBlockBytes.length));
  chunks.push(recentBlockBytes);

  // 9. Chain ID (uint64)
  chunks.push(writeUint64LE(tx.chain_id));

  // Ghép các chunk lại thành Buffer nhị phân hoàn chỉnh
  const totalLength = chunks.reduce((acc, c) => acc + c.length, 0);
  const serialized = new Uint8Array(totalLength);
  let offset = 0;
  for (const chunk of chunks) {
    serialized.set(chunk, offset);
    offset += chunk.length;
  }

  // Băm Blake3 DeriveKey tương thích hoàn toàn với Go/Rust Core
  const context = new TextEncoder().encode("BTC GenZ Toi Gian PoW v1.0");
  return blake3(serialized, { dkLen: 32, context });
}

// 7. Ký giao dịch ngoại tuyến (Offline Signing)
export function signTransactionOffline(tx: UnsignedTx, privateKey: Uint8Array): SignedTx {
  // 1. Tính toán signing hash
  const hash = getTransactionSigningHash(tx);

  // 2. Ký mã băm bằng Private Key Ed25519 (chú ý: privateKey trong tweetnacl chứa 64 bytes)
  // Trong đó 32 bytes đầu là seed/private key và 32 bytes sau là public key.
  const sig = nacl.sign.detached(hash, privateKey);

  return {
    ...tx,
    signature: '0x' + bytesToHex(sig)
  };
}

// 8. Ký trực tiếp mã băm hợp đồng nháp từ Node (Draft Signing Pattern)
export function signPreparedHash(signingHashHex: string, privateKey: Uint8Array): string {
  const cleanHex = signingHashHex.replace(/^0x/i, '');
  const hashBytes = hexToBytes(cleanHex);
  const sig = nacl.sign.detached(hashBytes, privateKey);
  return '0x' + bytesToHex(sig);
}

