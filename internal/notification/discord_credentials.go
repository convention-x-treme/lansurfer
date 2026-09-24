package notification

import (
	"encoding/json"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/platform/crypto"
)

// discordCredentialVersion is the version of the plaintext shape serialized
// below before encryption. It is carried inside the encrypted envelope
// itself (not as a separate database column) so a future format change can
// be detected on decrypt without a schema migration.
const discordCredentialVersion = 1

// discordCredentialEnvelope is the plaintext shape encrypted into a
// DiscordCredentialCiphertext. It exists only in memory: EncryptDiscordCredential
// and DecryptDiscordCredential are the only functions that ever construct or
// read one.
type discordCredentialEnvelope struct {
	Version      int    `json:"version"`
	WebhookID    string `json:"webhook_id"`
	WebhookToken string `json:"webhook_token"`
}

// EncryptDiscordCredential seals credential's webhook ID and token together
// as one versioned AES-256-GCM blob under key, the operator's Track E
// data-encryption key. Only the returned ciphertext is meant to be
// persisted; credential itself must never be logged or stored in the
// clear.
func EncryptDiscordCredential(key []byte, credential DiscordWebhookCredential) (DiscordCredentialCiphertext, error) {
	plaintext, err := json.Marshal(discordCredentialEnvelope{
		Version: discordCredentialVersion, WebhookID: credential.WebhookID, WebhookToken: credential.WebhookToken,
	})
	if err != nil {
		return nil, fmt.Errorf("encode discord webhook credential: %w", err)
	}
	ciphertext, err := crypto.Encrypt(key, string(plaintext))
	if err != nil {
		return nil, fmt.Errorf("encrypt discord webhook credential: %w", err)
	}
	return DiscordCredentialCiphertext(ciphertext), nil
}

// DecryptDiscordCredential reverses EncryptDiscordCredential. It fails
// closed (returns an error without partial data) for a wrong key, a
// corrupt/tampered ciphertext, or an unrecognized envelope version. Errors
// never include the plaintext webhook ID/token: crypto.Decrypt's GCM
// authentication failure carries no plaintext, and a version mismatch only
// names the numeric version found.
func DecryptDiscordCredential(key []byte, ciphertext DiscordCredentialCiphertext) (DiscordWebhookCredential, error) {
	plaintext, err := crypto.Decrypt(key, []byte(ciphertext))
	if err != nil {
		return DiscordWebhookCredential{}, fmt.Errorf("decrypt discord webhook credential: %w", err)
	}
	var envelope discordCredentialEnvelope
	if err := json.Unmarshal([]byte(plaintext), &envelope); err != nil {
		return DiscordWebhookCredential{}, fmt.Errorf("decode discord webhook credential: %w", err)
	}
	if envelope.Version != discordCredentialVersion {
		return DiscordWebhookCredential{}, fmt.Errorf("unsupported discord webhook credential version %d", envelope.Version)
	}
	return DiscordWebhookCredential{WebhookID: envelope.WebhookID, WebhookToken: envelope.WebhookToken}, nil
}
