package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters recommended by OWASP.
const (
	argonMemoryKiB   = 46 * 1024
	argonIterations  = 1
	argonParallelism = 1
	argonSaltBytes   = 16
	argonKeyBytes    = 32
)

// ErrHashFormat means a stored hash could not be parsed.
var ErrHashFormat = errors.New("invalid password hash format")

// PasswordHasher hashes passwords with Argon2id. Hashing is expensive by design,
// so concurrency is limited to keep login floods from exhausting the server.
type PasswordHasher struct {
	slots chan struct{}
	dummy string
}

// NewPasswordHasher creates a hasher.
func NewPasswordHasher() (*PasswordHasher, error) {
	hasher := &PasswordHasher{slots: make(chan struct{}, max(2, runtime.NumCPU()))}
	dummy, err := hasher.Hash(context.Background(), "dummy-password-for-constant-time-checks")
	if err != nil {
		return nil, err
	}
	hasher.dummy = dummy
	return hasher, nil
}

func (h *PasswordHasher) acquire(ctx context.Context) (func(), error) {
	select {
	case h.slots <- struct{}{}:
		return func() { <-h.slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for password hashing slot: %w", ctx.Err())
	}
}

// Hash returns the encoded Argon2id hash of a password.
func (h *PasswordHasher) Hash(ctx context.Context, password string) (string, error) {
	release, err := h.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	salt := make([]byte, argonSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemoryKiB, argonParallelism, argonKeyBytes)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemoryKiB, argonIterations,
		argonParallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify compares a password with an encoded hash.
func (h *PasswordHasher) Verify(ctx context.Context, password, encoded string) (bool, error) {
	release, err := h.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer release()

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrHashFormat
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, ErrHashFormat
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrHashFormat
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) == 0 || len(expected) > 64 {
		return false, ErrHashFormat
	}
	key := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected))) //nolint:gosec // bounded above
	return subtle.ConstantTimeCompare(key, expected) == 1, nil
}

// VerifyDummy spends the same time as a real check. It hides whether an account
// exists when no stored hash is available.
func (h *PasswordHasher) VerifyDummy(ctx context.Context, password string) {
	_, _ = h.Verify(ctx, password, h.dummy)
}

// NeedsRehash reports whether a hash was created with older parameters.
func (h *PasswordHasher) NeedsRehash(encoded string) bool {
	return !strings.Contains(encoded, fmt.Sprintf("$m=%d,t=%d,p=%d$", argonMemoryKiB, argonIterations, argonParallelism))
}
