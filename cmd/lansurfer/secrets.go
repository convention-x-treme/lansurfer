package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	webpush "github.com/marknefedov/go-webpush/v2"

	"github.com/convention-x-treme/lansurfer/internal/platform/crypto"
)

// writeNotificationSecrets writes fresh env-ready notification secrets to
// output. It never persists or logs them.
func writeNotificationSecrets(output io.Writer) error {
	vapid, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return fmt.Errorf("generate VAPID keys: %w", err)
	}
	private, err := vapid.PrivateKey().ECDH()
	if err != nil {
		return fmt.Errorf("encode VAPID private key: %w", err)
	}
	dataKey := make([]byte, crypto.KeySize)
	if _, err := rand.Read(dataKey); err != nil {
		return fmt.Errorf("generate data encryption key: %w", err)
	}
	_, err = fmt.Fprintf(output,
		"LANSURFER_WEBPUSH_PUBLIC_KEY=%s\nLANSURFER_WEBPUSH_PRIVATE_KEY=%s\nLANSURFER_DATA_ENCRYPTION_KEY=%s\n",
		vapid.PublicKeyString(),
		base64.RawURLEncoding.EncodeToString(private.Bytes()),
		base64.StdEncoding.EncodeToString(dataKey),
	)
	return err
}
