package keystore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// KeyEntry represents a key entry in the keystore index
type KeyEntry struct {
	Alias        string    `json:"alias"`
	PubKeyHex    string    `json:"pubKeyHex"`
	PubKeyHash   string    `json:"pubKeyHash"`
	CreatedAt    time.Time `json:"createdAt"`
	EncryptedKey string    `json:"encryptedKey"`
}

// Keystore manages Ed25519 keys with file-based storage
type Keystore struct {
	path      string
	indexPath string
	entries   map[string]*KeyEntry
	password  []byte
}

// New creates a new keystore at the specified path
func New(path string) (*Keystore, error) {
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, fmt.Errorf("failed to create keystore directory: %w", err)
	}

	indexPath := filepath.Join(path, "index.json")

	ks := &Keystore{
		path:      path,
		indexPath: indexPath,
		entries:   make(map[string]*KeyEntry),
		password:  getOSUserSecret(),
	}

	// Load existing index if it exists
	if err := ks.loadIndex(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load keystore index: %w", err)
	}

	return ks, nil
}

// getOSUserSecret derives a password from OS-specific user information
func getOSUserSecret() []byte {
	var secret string

	switch runtime.GOOS {
	case "windows":
		// Use username and computer name
		if user := os.Getenv("USERNAME"); user != "" {
			secret += user
		}
		if computer := os.Getenv("COMPUTERNAME"); computer != "" {
			secret += computer
		}
	case "darwin", "linux":
		// Use username and hostname
		if user := os.Getenv("USER"); user != "" {
			secret += user
		}
		if hostname := os.Getenv("HOSTNAME"); hostname != "" {
			secret += hostname
		}
	}

	// If no OS-specific info available, use a default (insecure)
	if secret == "" {
		secret = "accumen-keystore-default"
	}

	// Hash to create consistent key
	hash := sha256.Sum256([]byte(secret))
	return hash[:]
}

// loadIndex loads the keystore index from disk
func (ks *Keystore) loadIndex() error {
	data, err := os.ReadFile(ks.indexPath)
	if err != nil {
		return err
	}

	var entries []*KeyEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to unmarshal index: %w", err)
	}

	ks.entries = make(map[string]*KeyEntry)
	for _, entry := range entries {
		ks.entries[entry.Alias] = entry
	}

	return nil
}

// saveIndex saves the keystore index to disk
func (ks *Keystore) saveIndex() error {
	entries := make([]*KeyEntry, 0, len(ks.entries))
	for _, entry := range ks.entries {
		entries = append(entries, entry)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	// Write with secure permissions
	if err := os.WriteFile(ks.indexPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	return nil
}

// encryptKey encrypts a private key using AES-GCM
func (ks *Keystore) encryptKey(privateKey ed25519.PrivateKey) (string, error) {
	if len(ks.password) == 0 {
		// No encryption available, return hex-encoded key with warning prefix
		return "plain:" + hex.EncodeToString(privateKey), nil
	}

	// Create AES cipher
	block, err := aes.NewCipher(ks.password)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nonce, nonce, privateKey, nil)
	return "aes:" + hex.EncodeToString(ciphertext), nil
}

// decryptKey decrypts a private key
func (ks *Keystore) decryptKey(encryptedKey string) (ed25519.PrivateKey, error) {
	if len(encryptedKey) < 6 {
		return nil, fmt.Errorf("invalid encrypted key format")
	}

	prefix := encryptedKey[:5]
	data := encryptedKey[5:]

	switch prefix {
	case "plain":
		// Plaintext key
		return hex.DecodeString(data)

	case "aes:":
		// AES-GCM encrypted key
		if len(ks.password) == 0 {
			return nil, fmt.Errorf("cannot decrypt key: no password available")
		}

		ciphertext, err := hex.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
		}

		block, err := aes.NewCipher(ks.password)
		if err != nil {
			return nil, fmt.Errorf("failed to create cipher: %w", err)
		}

		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("failed to create GCM: %w", err)
		}

		if len(ciphertext) < gcm.NonceSize() {
			return nil, fmt.Errorf("ciphertext too short")
		}

		nonce := ciphertext[:gcm.NonceSize()]
		ciphertext = ciphertext[gcm.NonceSize():]

		plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt key: %w", err)
		}

		return ed25519.PrivateKey(plaintext), nil

	default:
		return nil, fmt.Errorf("unknown encryption format: %s", prefix)
	}
}

// Create generates a new Ed25519 key pair and stores it with the given alias
func (ks *Keystore) Create(alias string) error {
	if _, exists := ks.entries[alias]; exists {
		return fmt.Errorf("key with alias '%s' already exists", alias)
	}

	// Generate new Ed25519 key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %w", err)
	}

	// Encrypt private key
	encryptedKey, err := ks.encryptKey(privKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt private key: %w", err)
	}

	// Calculate public key hash
	pubKeyHash := sha256.Sum256(pubKey)

	// Create entry
	entry := &KeyEntry{
		Alias:        alias,
		PubKeyHex:    hex.EncodeToString(pubKey),
		PubKeyHash:   hex.EncodeToString(pubKeyHash[:]),
		CreatedAt:    time.Now().UTC(),
		EncryptedKey: encryptedKey,
	}

	// Store entry
	ks.entries[alias] = entry

	// Save index
	if err := ks.saveIndex(); err != nil {
		delete(ks.entries, alias)
		return fmt.Errorf("failed to save keystore index: %w", err)
	}

	return nil
}

// Import imports an existing private key with the given alias
func (ks *Keystore) Import(alias, privHex string) error {
	if _, exists := ks.entries[alias]; exists {
		return fmt.Errorf("key with alias '%s' already exists", alias)
	}

	// Decode private key
	privKeyBytes, err := hex.DecodeString(privHex)
	if err != nil {
		return fmt.Errorf("invalid private key hex: %w", err)
	}

	if len(privKeyBytes) != ed25519.PrivateKeySize {
		return fmt.Errorf("private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(privKeyBytes))
	}

	privKey := ed25519.PrivateKey(privKeyBytes)
	pubKey := privKey.Public().(ed25519.PublicKey)

	// Encrypt private key
	encryptedKey, err := ks.encryptKey(privKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt private key: %w", err)
	}

	// Calculate public key hash
	pubKeyHash := sha256.Sum256(pubKey)

	// Create entry
	entry := &KeyEntry{
		Alias:        alias,
		PubKeyHex:    hex.EncodeToString(pubKey),
		PubKeyHash:   hex.EncodeToString(pubKeyHash[:]),
		CreatedAt:    time.Now().UTC(),
		EncryptedKey: encryptedKey,
	}

	// Store entry
	ks.entries[alias] = entry

	// Save index
	if err := ks.saveIndex(); err != nil {
		delete(ks.entries, alias)
		return fmt.Errorf("failed to save keystore index: %w", err)
	}

	return nil
}

// Export exports the private key for the given alias
func (ks *Keystore) Export(alias string) (string, error) {
	entry, exists := ks.entries[alias]
	if !exists {
		return "", fmt.Errorf("key with alias '%s' not found", alias)
	}

	// Decrypt private key
	privKey, err := ks.decryptKey(entry.EncryptedKey)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt private key: %w", err)
	}

	return hex.EncodeToString(privKey), nil
}

// List returns all key entries
func (ks *Keystore) List() []*KeyEntry {
	entries := make([]*KeyEntry, 0, len(ks.entries))
	for _, entry := range ks.entries {
		// Return a copy without the encrypted key
		entryCopy := *entry
		entryCopy.EncryptedKey = "" // Don't expose encrypted key in list
		entries = append(entries, &entryCopy)
	}
	return entries
}

// Delete removes a key from the keystore
func (ks *Keystore) Delete(alias string) error {
	if _, exists := ks.entries[alias]; !exists {
		return fmt.Errorf("key with alias '%s' not found", alias)
	}

	delete(ks.entries, alias)

	// Save index
	if err := ks.saveIndex(); err != nil {
		return fmt.Errorf("failed to save keystore index: %w", err)
	}

	return nil
}

// PubKeyHash returns the public key hash for the given alias
func (ks *Keystore) PubKeyHash(alias string) ([]byte, error) {
	entry, exists := ks.entries[alias]
	if !exists {
		return nil, fmt.Errorf("key with alias '%s' not found", alias)
	}

	hash, err := hex.DecodeString(entry.PubKeyHash)
	if err != nil {
		return nil, fmt.Errorf("failed to decode public key hash: %w", err)
	}

	return hash, nil
}

// GetPublicKey returns the public key for the given alias
func (ks *Keystore) GetPublicKey(alias string) (ed25519.PublicKey, error) {
	entry, exists := ks.entries[alias]
	if !exists {
		return nil, fmt.Errorf("key with alias '%s' not found", alias)
	}

	pubKeyBytes, err := hex.DecodeString(entry.PubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode public key: %w", err)
	}

	return ed25519.PublicKey(pubKeyBytes), nil
}

// GetPrivateKey returns the private key for the given alias
func (ks *Keystore) GetPrivateKey(alias string) (ed25519.PrivateKey, error) {
	entry, exists := ks.entries[alias]
	if !exists {
		return nil, fmt.Errorf("key with alias '%s' not found", alias)
	}

	return ks.decryptKey(entry.EncryptedKey)
}

// HasKey checks if a key with the given alias exists
func (ks *Keystore) HasKey(alias string) bool {
	_, exists := ks.entries[alias]
	return exists
}

// Path returns the keystore directory path
func (ks *Keystore) Path() string {
	return ks.path
}

// ChangePassword changes the encryption password and re-encrypts all keys
func (ks *Keystore) ChangePassword(newPassword []byte) error {
	// Decrypt all private keys with old password
	var privKeys []ed25519.PrivateKey
	var aliases []string

	for alias, entry := range ks.entries {
		privKey, err := ks.decryptKey(entry.EncryptedKey)
		if err != nil {
			return fmt.Errorf("failed to decrypt key '%s' with old password: %w", alias, err)
		}
		privKeys = append(privKeys, privKey)
		aliases = append(aliases, alias)
	}

	// Change password
	oldPassword := ks.password
	ks.password = newPassword

	// Re-encrypt all keys with new password
	for i, alias := range aliases {
		encryptedKey, err := ks.encryptKey(privKeys[i])
		if err != nil {
			// Restore old password on failure
			ks.password = oldPassword
			return fmt.Errorf("failed to encrypt key '%s' with new password: %w", alias, err)
		}
		ks.entries[alias].EncryptedKey = encryptedKey
	}

	// Save index with re-encrypted keys
	if err := ks.saveIndex(); err != nil {
		// Restore old password on failure
		ks.password = oldPassword
		return fmt.Errorf("failed to save keystore with new password: %w", err)
	}

	return nil
}

// Backup creates a backup of the keystore
func (ks *Keystore) Backup(backupPath string) error {
	// Create backup directory
	if err := os.MkdirAll(backupPath, 0700); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Copy index file
	srcIndex := ks.indexPath
	dstIndex := filepath.Join(backupPath, "index.json")

	data, err := os.ReadFile(srcIndex)
	if err != nil {
		return fmt.Errorf("failed to read source index: %w", err)
	}

	if err := os.WriteFile(dstIndex, data, 0600); err != nil {
		return fmt.Errorf("failed to write backup index: %w", err)
	}

	return nil
}

// Restore restores the keystore from a backup
func (ks *Keystore) Restore(backupPath string) error {
	backupIndex := filepath.Join(backupPath, "index.json")

	// Check if backup exists
	if _, err := os.Stat(backupIndex); err != nil {
		return fmt.Errorf("backup index not found: %w", err)
	}

	// Load backup
	data, err := os.ReadFile(backupIndex)
	if err != nil {
		return fmt.Errorf("failed to read backup index: %w", err)
	}

	// Backup current keystore
	currentBackup := ks.indexPath + ".bak"
	if currentData, err := os.ReadFile(ks.indexPath); err == nil {
		os.WriteFile(currentBackup, currentData, 0600)
	}

	// Write restored data
	if err := os.WriteFile(ks.indexPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write restored index: %w", err)
	}

	// Reload index
	if err := ks.loadIndex(); err != nil {
		// Restore backup on failure
		if currentData, err := os.ReadFile(currentBackup); err == nil {
			os.WriteFile(ks.indexPath, currentData, 0600)
		}
		return fmt.Errorf("failed to load restored index: %w", err)
	}

	// Remove temporary backup
	os.Remove(currentBackup)

	return nil
}
