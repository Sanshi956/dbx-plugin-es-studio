//go:build !windows

package mcp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Non-Windows fallback. There is no per-user equivalent of DPAPI that works
// without a keyring dependency, so the grant is sealed with a 256-bit key file
// created 0600 next to it. This is documented as a platform limitation instead
// of pretending the protection is equivalent (SPEC §5.2 covers Windows DPAPI).
const (
	sealAlgorithm   = "aesgcm-filekey"
	sealKeyFilename = "mcp-grant.key"
)

func keyPath() (string, error) {
	dir := os.Getenv("DBX_PLUGIN_DATA_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "es-studio")
	}
	return filepath.Join(dir, sealKeyFilename), nil
}

func loadKey() ([]byte, error) {
	path, err := keyPath()
	if err != nil {
		return nil, err
	}
	if data, err := os.ReadFile(path); err == nil && len(data) == 32 {
		return data, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func seal(plain []byte) ([]byte, string, error) {
	key, err := loadKey()
	if err != nil {
		return nil, "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", err
	}
	return gcm.Seal(nonce, nonce, plain, nil), sealAlgorithm, nil
}

func unseal(sealed []byte, algorithm string) ([]byte, error) {
	if algorithm != sealAlgorithm {
		return nil, fmt.Errorf("grant was sealed with %q, this build can only open %q", algorithm, sealAlgorithm)
	}
	key, err := loadKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("sealed grant is truncated")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
}
