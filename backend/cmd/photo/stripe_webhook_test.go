package main

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"photoeditor/backend/internal/config"
)

func TestStripeWebhookDisabledDoesNotOpenSecretsOrDatabase(t *testing.T) {
	service, close, err := installStripeWebhookIngress(context.Background(), config.StripeIngressConfig{WebhookSecretRef: "/missing/secret", DatabaseURL: "invalid"})
	if err != nil || service != nil || close == nil {
		t.Fatal("disabled ingress made effects", err)
	}
	close()
}
func TestPrivateStripeFilesRejectPublicDirectoryAndOversizedInput(t *testing.T) {
	directory := t.TempDir()
	for _, tc := range []struct {
		name string
		mode os.FileMode
		size int
	}{{"public", 0644, 10}, {"large", 0600, 4097}} {
		path := filepath.Join(directory, tc.name)
		if err := os.WriteFile(path, []byte(strings.Repeat("s", tc.size)), tc.mode); err != nil {
			t.Fatal(err)
		}
		if _, err := readPrivateStripeFile(path); err == nil {
			t.Fatal("unsafe secret file accepted", tc.name)
		}
	}
	if _, err := readPrivateStripeFile(directory); err == nil {
		t.Fatal("directory accepted as secret")
	}
}
func TestStripeWebhookInstallerDoesNotExposeSecretOnInvalidConfiguration(t *testing.T) {
	directory := t.TempDir()
	signing := filepath.Join(directory, "signing")
	key := filepath.Join(directory, "key")
	const privateValue = "whsec_privatefixturevalue"
	if err := os.WriteFile(signing, []byte(privateValue), 0600); err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{"private-invalid-payload-value", base64.StdEncoding.EncodeToString(make([]byte, 32))} {
		if err := os.WriteFile(key, []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
		service, close, err := installStripeWebhookIngress(context.Background(), config.StripeIngressConfig{Enabled: true, WebhookSecretRef: signing, PayloadKeyRef: key, DatabaseURL: "invalid connection value", PayloadKeyID: "fixture"})
		if close != nil {
			close()
		}
		if err == nil || service != nil {
			t.Fatal("invalid installation accepted")
		}
		if strings.Contains(err.Error(), privateValue) || strings.Contains(err.Error(), payload) {
			t.Fatal("secret leaked in error")
		}
	}
}
