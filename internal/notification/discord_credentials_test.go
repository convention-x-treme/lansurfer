package notification

import (
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/platform/crypto"
)

func testDiscordKey(seed byte) []byte {
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = seed + byte(i)
	}
	return key
}

func TestDiscordCredentialEncryptDecryptRoundTrips(t *testing.T) {
	key := testDiscordKey(1)
	credential := DiscordWebhookCredential{WebhookID: "webhook-id-123", WebhookToken: "webhook-token-secret"}

	ciphertext, err := EncryptDiscordCredential(key, credential)
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	if len(ciphertext) == 0 {
		t.Fatal("EncryptDiscordCredential() returned empty ciphertext")
	}
	if strings.Contains(string(ciphertext), credential.WebhookID) || strings.Contains(string(ciphertext), credential.WebhookToken) {
		t.Fatal("ciphertext contains the plaintext webhook ID or token")
	}

	decrypted, err := DecryptDiscordCredential(key, ciphertext)
	if err != nil {
		t.Fatalf("DecryptDiscordCredential() error = %v", err)
	}
	if decrypted != credential {
		t.Fatalf("DecryptDiscordCredential() = %#v, want %#v", decrypted, credential)
	}
}

func TestDiscordCredentialDecryptFailsClosedWithWrongKey(t *testing.T) {
	ciphertext, err := EncryptDiscordCredential(testDiscordKey(1), DiscordWebhookCredential{WebhookID: "id", WebhookToken: "token-secret"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}

	_, err = DecryptDiscordCredential(testDiscordKey(2), ciphertext)
	if err == nil {
		t.Fatal("DecryptDiscordCredential() with the wrong key succeeded, want an error")
	}
	if strings.Contains(err.Error(), "token-secret") {
		t.Fatalf("error leaked the plaintext webhook token: %v", err)
	}
}

func TestDiscordCredentialDecryptFailsClosedWithCorruptCiphertext(t *testing.T) {
	key := testDiscordKey(3)
	ciphertext, err := EncryptDiscordCredential(key, DiscordWebhookCredential{WebhookID: "id", WebhookToken: "token-secret"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	corrupted := append([]byte{}, ciphertext...)
	corrupted[len(corrupted)-1] ^= 0xFF

	_, err = DecryptDiscordCredential(key, DiscordCredentialCiphertext(corrupted))
	if err == nil {
		t.Fatal("DecryptDiscordCredential() with corrupted ciphertext succeeded, want an error")
	}
	if strings.Contains(err.Error(), "token-secret") {
		t.Fatalf("error leaked the plaintext webhook token: %v", err)
	}
}

func TestDiscordCredentialDecryptFailsClosedOnTruncatedCiphertext(t *testing.T) {
	key := testDiscordKey(4)
	if _, err := DecryptDiscordCredential(key, DiscordCredentialCiphertext([]byte{1, 2, 3})); err == nil {
		t.Fatal("DecryptDiscordCredential() with truncated ciphertext succeeded, want an error")
	}
}
