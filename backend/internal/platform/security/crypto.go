package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

const tokenBytes = 32

// NewToken returns a random URL-safe token with 256 bits of entropy.
func NewToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashToken returns the digest stored in place of a high-entropy token. A slow
// hash is unnecessary here, because the input cannot be guessed.
func HashToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}

// Sealer encrypts small secrets with AES-256-GCM.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer creates a Sealer from a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must have 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create aead: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext. The context binds the ciphertext to its owner, so a
// value copied to another row cannot be decrypted.
func (s *Sealer) Seal(plaintext, context []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, context), nil
}

// Open decrypts a value produced by Seal with the same context.
func (s *Sealer) Open(sealed, context []byte) ([]byte, error) {
	size := s.aead.NonceSize()
	if len(sealed) < size {
		return nil, errors.New("sealed value is too short")
	}
	plaintext, err := s.aead.Open(nil, sealed[:size], sealed[size:], context)
	if err != nil {
		return nil, fmt.Errorf("decrypt value: %w", err)
	}
	return plaintext, nil
}
