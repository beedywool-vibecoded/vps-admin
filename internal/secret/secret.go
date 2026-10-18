// Package secret encrypts values stored in the database (environment variables, the
// GitHub App key, API keys) with a key kept in a separate 0600 file of the data dir. A
// copy of the database alone does not reveal them.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const prefix = "v1:"

type Box struct {
	aead cipher.AEAD
}

// Open loads data_dir/secret.key, creating it on first use.
func Open(dataDir string) (*Box, error) {
	path := filepath.Join(dataDir, "secret.key")
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		rand.Read(key)
		if err := os.WriteFile(path, key, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("secret.key must hold 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Seal(plain string) string {
	if plain == "" {
		return ""
	}
	nonce := make([]byte, b.aead.NonceSize())
	rand.Read(nonce)
	return prefix + base64.StdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil))
}

func (b *Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, prefix))
	if err != nil || !strings.HasPrefix(sealed, prefix) {
		return "", errors.New("not a sealed value")
	}
	n := b.aead.NonceSize()
	if len(raw) < n {
		return "", errors.New("sealed value too short")
	}
	out, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	return string(out), err
}
