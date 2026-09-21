package crypto

import (
	"encoding/base64"
	"testing"
)

func TestParseKeyRequiresExactly32Bytes(t *testing.T) {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	parsed, err := ParseKey(encoded)
	if err != nil {
		t.Fatalf("ParseKey() error = %v", err)
	}
	if len(parsed) != KeySize {
		t.Fatalf("ParseKey() length = %d, want %d", len(parsed), KeySize)
	}

	if _, err := ParseKey(base64.StdEncoding.EncodeToString(key[:16])); err == nil {
		t.Fatal("ParseKey() with a 16-byte key succeeded, want an error")
	}
	if _, err := ParseKey("not base64!!"); err == nil {
		t.Fatal("ParseKey() with invalid base64 succeeded, want an error")
	}
}

func TestEncryptDecryptRoundTrips(t *testing.T) {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i * 3)
	}
	ciphertext, err := Encrypt(key, "https://push.example/endpoint/abc123")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	plaintext, err := Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if plaintext != "https://push.example/endpoint/abc123" {
		t.Fatalf("Decrypt() = %q, want the original plaintext", plaintext)
	}
}

func TestEncryptProducesDistinctCiphertextsForTheSamePlaintext(t *testing.T) {
	key := make([]byte, KeySize)
	first, err := Encrypt(key, "same plaintext")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	second, err := Encrypt(key, "same plaintext")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if string(first) == string(second) {
		t.Fatal("Encrypt() produced identical ciphertext twice; the nonce is not being randomized")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	key := make([]byte, KeySize)
	ciphertext, err := Encrypt(key, "authentic")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	tampered := append([]byte{}, ciphertext...)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := Decrypt(key, tampered); err == nil {
		t.Fatal("Decrypt() accepted tampered ciphertext, want authentication failure")
	}
}

func TestFingerprintIsDeterministicAndKeyed(t *testing.T) {
	keyA := make([]byte, KeySize)
	keyB := make([]byte, KeySize)
	keyB[0] = 1
	first := Fingerprint(keyA, "https://push.example/endpoint/abc123")
	second := Fingerprint(keyA, "https://push.example/endpoint/abc123")
	if first != second {
		t.Fatalf("Fingerprint() is not deterministic: %q != %q", first, second)
	}
	if Fingerprint(keyA, "https://push.example/endpoint/abc123") == Fingerprint(keyB, "https://push.example/endpoint/abc123") {
		t.Fatal("Fingerprint() did not change with the key")
	}
	if Fingerprint(keyA, "https://push.example/endpoint/xyz789") == first {
		t.Fatal("Fingerprint() did not change with the input")
	}
}
