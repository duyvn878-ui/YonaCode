import nacl from 'tweetnacl';
import { blake3 } from '@noble/hashes/blake3.js';
import { sha256 } from '@noble/hashes/sha2.js';

// Helpers
function hexToBytes(hex) {
  const cleanHex = hex.replace(/^0x/, '');
  const bytes = new Uint8Array(cleanHex.length / 2);
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(cleanHex.substr(i * 2, 2), 16);
  }
  return bytes;
}

function bytesToHex(bytes) {
  return Array.from(bytes)
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');
}

function writeUint64LE(value) {
  const buf = new ArrayBuffer(8);
  const view = new DataView(buf);
  const low = value % 0x100000000;
  const high = Math.floor(value / 0x100000000);
  view.setUint32(0, low, true);
  view.setUint32(4, high, true);
  return new Uint8Array(buf);
}

function writeUint32LE(value) {
  const buf = new ArrayBuffer(4);
  const view = new DataView(buf);
  view.setUint32(0, value, true);
  return new Uint8Array(buf);
}

function hexToRaw32Bytes(hex) {
  const cleanBytes = hexToBytes(hex);
  if (cleanBytes.length === 33) {
    return cleanBytes.slice(1);
  }
  if (cleanBytes.length > 32) {
    return cleanBytes.slice(cleanBytes.length - 32);
  }
  return cleanBytes;
}

const tx = {
  version: 1,
  sender: "0x036c3d7f58dde5f38f7fea598058f2def699157cb401268484d0ff896203ef9f",
  receiver: "0xd253f4d1e9567a181c28bcc280f6d3ef2b8cbe373043f7bd8076aa0e15ef50c8",
  amount: 100000000,
  fee: 250,
  nonce: 0,
  timestamp: 1784545099,
  recent_block_hash: "0000000000000000000000000000000000000000000000000000000000000000",
  chain_id: 25062025
};

const chunks = [];
chunks.push(writeUint64LE(tx.version));

const senderBytes = hexToRaw32Bytes(tx.sender);
chunks.push(writeUint32LE(senderBytes.length));
chunks.push(senderBytes);

const receiverBytes = hexToRaw32Bytes(tx.receiver);
chunks.push(writeUint32LE(receiverBytes.length));
chunks.push(receiverBytes);

chunks.push(writeUint64LE(tx.amount));
chunks.push(writeUint64LE(tx.fee));
chunks.push(writeUint64LE(tx.nonce));
chunks.push(writeUint64LE(tx.timestamp));

const recentBlockBytes = hexToBytes(tx.recent_block_hash);
chunks.push(writeUint32LE(recentBlockBytes.length));
chunks.push(recentBlockBytes);

chunks.push(writeUint64LE(tx.chain_id));

const totalLength = chunks.reduce((acc, c) => acc + c.length, 0);
const serialized = new Uint8Array(totalLength);
let offset = 0;
for (const chunk of chunks) {
  serialized.set(chunk, offset);
  offset += chunk.length;
}

const context = new TextEncoder().encode("BTC GenZ Toi Gian PoW v1.0");
const signingHash = blake3(serialized, { dkLen: 32, context });

console.log("JS HASH FOR NONCE 0 TX:", bytesToHex(signingHash));
