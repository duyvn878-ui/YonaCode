package internal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"


	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/argon2"
)

type Wallet struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	Encrypted string `json:"encrypted_seed"`
	Version   int    `json:"version,omitempty"` // 0: SHA256 (ví cũ), 1: Argon2id
	Salt      string `json:"salt,omitempty"`    // Salt ngẫu nhiên cho Argon2id
}

type WalletManager struct {
	DataDir string
}

func NewWalletManager(dataDir string) *WalletManager {
	os.MkdirAll(dataDir, 0700)
	return &WalletManager{DataDir: dataDir}
}

func (wm *WalletManager) CreateWallet(name, password, passphrase string) (string, string, error) {
	entropy, _ := bip39.NewEntropy(128)
	mnemonic, _ := bip39.NewMnemonic(entropy)
	
	// [V2.2 ORIGINAL-SHA256] 1 Mnemonic = 1 Unique Address (Legacy Matrix V1)
	// Formula: BIP39 Seed -> SHA256 -> Ed25519 Keypair
	seed := bip39.NewSeed(mnemonic, passphrase)
	
	hash := sha256.Sum256(seed)
	hashedSeed := hash[:]
	
	priv := ed25519.NewKeyFromSeed(hashedSeed)
	pub := priv.Public().(ed25519.PublicKey)
	addr := fmt.Sprintf("%x", pub)

	encrypted, salt, err := wm.encryptV2(hashedSeed, password)
	if err != nil { return "", "", err }

	wallet := Wallet{Name: name, Address: addr, Encrypted: encrypted, Version: 1, Salt: salt}
	err = wm.saveWallet(wallet)
	
	return mnemonic, addr, err
}

func (wm *WalletManager) RestoreWallet(mnemonic, name, password, passphrase string) (string, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return "", fmt.Errorf("Seed phrase không hợp lệ")
	}
	
	// [V2.2 ORIGINAL-SHA256] Restore forced to standard SHA256 logic
	seed := bip39.NewSeed(mnemonic, passphrase)
	hash := sha256.Sum256(seed)
	hashedSeed := hash[:]
	
	priv := ed25519.NewKeyFromSeed(hashedSeed)
	pub := priv.Public().(ed25519.PublicKey)
	addr := fmt.Sprintf("%x", pub)

	encrypted, salt, err := wm.encryptV2(hashedSeed, password)
	if err != nil { return "", err }

	wallet := Wallet{Name: name, Address: addr, Encrypted: encrypted, Version: 1, Salt: salt}
	err = wm.saveWallet(wallet)
	
	return addr, err
}

func (wm *WalletManager) DeriveAddressOnly(mnemonic, passphrase string) (string, error) {
	seed := bip39.NewSeed(mnemonic, passphrase)
	hash := sha256.Sum256(seed)
	hashedSeed := hash[:]
	
	priv := ed25519.NewKeyFromSeed(hashedSeed)
	pub := priv.Public().(ed25519.PublicKey)
	return fmt.Sprintf("%x", pub), nil
}

func (wm *WalletManager) GetSeed(address, password string) ([]byte, error) {
	wallet, err := wm.LoadWallet(address)
	if err != nil { return nil, err }
	
	return wm.decryptV2(wallet.Encrypted, wallet.Salt, password)
}

func (wm *WalletManager) ListWallets() ([]Wallet, error) {
	files, _ := os.ReadDir(wm.DataDir)
	var wallets []Wallet
	for _, f := range files {
		if filepath.Ext(f.Name()) == ".json" {
			data, _ := os.ReadFile(filepath.Join(wm.DataDir, f.Name()))
			var w Wallet
			if err := json.Unmarshal(data, &w); err == nil {
				wallets = append(wallets, w)
			}
		}
	}
	return wallets, nil
}


// V2 (Argon2id)
func (wm *WalletManager) encryptV2(data []byte, password string) (string, string, error) {
	salt := make([]byte, 16)
	io.ReadFull(rand.Reader, salt)
	
	key := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return fmt.Sprintf("%x", ciphertext), fmt.Sprintf("%x", salt), nil
}

func (wm *WalletManager) decryptV2(hexData, hexSalt, password string) ([]byte, error) {
	data, _ := hex.DecodeString(hexData)
	salt, _ := hex.DecodeString(hexSalt)
	
	key := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonceSize := gcm.NonceSize()
	
	if len(data) < nonceSize { 
		return nil, fmt.Errorf("Dữ liệu khóa bị hỏng (V1)") 
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	decrypted, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("Sai mật khẩu hoặc ví không tồn tại!")
	}
	return decrypted, nil
}




func (wm *WalletManager) saveWallet(w Wallet) error {
	os.MkdirAll(wm.DataDir, 0700)
	path := filepath.Join(wm.DataDir, w.Address+".json")
	data, _ := json.MarshalIndent(w, "", "  ")
	return os.WriteFile(path, data, 0600)
}

func (wm *WalletManager) LoadWallet(address string) (Wallet, error) {
	path := filepath.Join(wm.DataDir, address+".json")
	data, err := os.ReadFile(path)
	if err != nil { return Wallet{}, err }
	var w Wallet
	err = json.Unmarshal(data, &w)
	return w, err
}

func (wm *WalletManager) DeleteWallet(address string) error {
	address = strings.TrimPrefix(address, "0x")
	address = strings.ToLower(strings.TrimSpace(address))
	path := filepath.Join(wm.DataDir, address+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("Ví không tồn tại hoặc đã bị xóa trước đó!")
	}
	return os.Remove(path)
}

