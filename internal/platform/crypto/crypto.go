// Package crypto provides the narrow AES-GCM/HMAC primitives the notification module uses to
// store external credentials (Web Push subscription material, Discord
// webhook tokens) at rest, and to derive a stable, non-reversible fingerprint
// for enforcing endpoint uniqueness without persisting the raw endpoint.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// KeySize is the required length, in bytes, of the operator-provided
// data-encryption key (AES-256).
const KeySize = 32

var ErrInvalidKey = fmt.Errorf("encryption key must decode to exactly %d bytes", KeySize)

// ParseKey decodes an operator-provided LANSURFER_DATA_ENCRYPTION_KEY value
// (standard base64) into a raw key suitable for Encrypt/Decrypt/Fingerprint.
func ParseKey(value string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	return key, nil
}

// Encrypt seals plaintext with AES-256-GCM under key, prepending a random
// nonce. Two calls with identical plaintext never produce identical output.
func Encrypt(key []byte, plaintext string) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt reverses Encrypt. It returns an error if ciphertext was truncated
// or has been tampered with (GCM authentication failure).
func Decrypt(key []byte, ciphertext []byte) (string, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < aead.NonceSize() {
		return "", errors.New("ciphertext is shorter than the nonce")
	}
	nonce, sealed := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

// Fingerprint derives a deterministic, keyed, non-reversible identifier for
// data (a Push endpoint URL) so a unique index can enforce "one endpoint
// belongs to at most one user" without ever storing the raw endpoint next to
// its own fingerprint.
func Fingerprint(key []byte, data string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// DeriveKey derives a fixed-length (KeySize) subkey from key for one
// specific purpose (e.g. "notification.push.endpoint-fingerprint",
// "notification.push.device-hash"), so a single data-encryption key never
// signs two different things with the same key material. key is treated as
// already-strong keying material (it is itself an AES-256 key), so it is
// used directly as HKDF's pseudorandom key; purpose is HKDF's "info" input.
func DeriveKey(key []byte, purpose string) ([]byte, error) {
	derived, err := hkdf.Expand(sha256.New, key, purpose, KeySize)
	if err != nil {
		return nil, fmt.Errorf("derive key for %q: %w", purpose, err)
	}
	return derived, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return aead, nil
}
