package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			_, closeRuntime, err := installProcessSigningRuntime(ctx, tc.path, tc.pin, tc.cert, tc.key, apiProcessDependencies{})
			if (err != nil) != tc.wantErr || closeRuntime == nil {
				t.Fatalf("err=%v closeRuntime=%v", err, closeRuntime != nil)
			}
			closeRuntime()
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
