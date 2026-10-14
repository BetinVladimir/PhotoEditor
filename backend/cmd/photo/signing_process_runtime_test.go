package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSigningProcessRuntimeRequiresPinnedIdentity(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, path, pin, cert, key string
		wantErr                    bool
	}{
		{name: "disabled"},
		{name: "orphan certificate", cert: "/cert", wantErr: true},
		{name: "orphan key", key: "/key", wantErr: true},
		{name: "missing identity", path: "/deployment", pin: strings.Repeat("a", 64), wantErr: true},
		{name: "partial identity", path: "/deployment", pin: strings.Repeat("a", 64), cert: "/cert", wantErr: true},
		{name: "invalid pin", path: "/deployment", pin: "BAD", cert: "/cert", key: "/key", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, closeRuntime, err := installProcessSigningRuntime(ctx, tc.path, tc.pin, tc.cert, tc.key, "", apiProcessDependencies{})
			if (err != nil) != tc.wantErr || closeRuntime == nil {
				t.Fatalf("err=%v closeRuntime=%v", err, closeRuntime != nil)
			}
			closeRuntime()
		})
	}
}

func TestSigningClientIdentityProfile(t *testing.T) {
	now := time.Now()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		before  time.Time
		after   time.Time
		ca      bool
		usage   x509.KeyUsage
		ext     []x509.ExtKeyUsage
		wantErr bool
	}{
		{"valid", now.Add(-time.Hour), now.Add(time.Hour), false, x509.KeyUsageDigitalSignature, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, false},
		{"expired", now.Add(-2 * time.Hour), now.Add(-time.Hour), false, x509.KeyUsageDigitalSignature, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, true},
		{"future", now.Add(time.Hour), now.Add(2 * time.Hour), false, x509.KeyUsageDigitalSignature, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, true},
		{"server only", now.Add(-time.Hour), now.Add(time.Hour), false, x509.KeyUsageDigitalSignature, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, true},
		{"missing digital signature", now.Add(-time.Hour), now.Add(time.Hour), false, x509.KeyUsageKeyEncipherment, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, true},
		{"CA leaf", now.Add(-time.Hour), now.Add(time.Hour), true, x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: tc.before, NotAfter: tc.after,
				BasicConstraintsValid: true, IsCA: tc.ca, KeyUsage: tc.usage, ExtKeyUsage: tc.ext}
			der, err := x509.CreateCertificate(rand.Reader, cert, cert, key.Public(), key)
			if err != nil {
				t.Fatal(err)
			}
			identity := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
			if err := validateSigningClientIdentity(identity, now); (err != nil) != tc.wantErr {
				t.Fatalf("identity profile: %v", err)
			}
		})
	}
}

func TestSigningIdentityFileSecurity(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	key := filepath.Join(dir, "identity.key")
	if err := os.WriteFile(key, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := readSigningIdentityFile(ctx, key, true); err != nil || string(data) != "secret" {
		t.Fatalf("private key read: %v", err)
	}
	if err := os.Chmod(key, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSigningIdentityFile(ctx, key, true); err == nil {
		t.Fatal("world-readable private key accepted")
	}
	link := filepath.Join(dir, "identity-link.key")
	if err := os.Symlink(key, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readSigningIdentityFile(ctx, link, false); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := readSigningIdentityFile(ctx, "relative.key", false); err == nil {
		t.Fatal("relative identity path accepted")
	}
}
