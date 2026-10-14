package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestInstallLocalInterchangePersonKey(t *testing.T) {
	seed := sha256.Sum256([]byte("api process person origin"))
	private := ed25519.NewKeyFromSeed(seed[:])
	public := private.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	path := filepath.Join(t.TempDir(), "person.seed")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(seed[:])), 0600); err != nil {
		t.Fatal(err)
	}
	issuer := uuid.New()
	base := apiProcessDependencies{}
	if _, err := installLocalInterchangePersonKey(context.Background(), path, "", issuer.String(), base); err == nil {
		t.Fatal("partial signer declaration accepted")
	}
	installed, err := installLocalInterchangePersonKey(context.Background(), path,
		hex.EncodeToString(digest[:]), issuer.String(), base)
	if err != nil || installed.InterchangePersonOriginKey == nil || installed.InterchangePersonIssuerID != issuer {
		t.Fatal("pinned signer was not installed", err)
	}
	if _, err := installLocalInterchangePersonKey(context.Background(), path,
		hex.EncodeToString(digest[:]), issuer.String(), installed); err == nil {
		t.Fatal("duplicate signer declaration accepted")
	}
}
