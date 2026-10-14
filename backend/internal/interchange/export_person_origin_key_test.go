package interchange

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestProcessPersonOriginKeyRequiresPrivatePinnedFile(t *testing.T) {
	seed := sha256.Sum256([]byte("operator test key"))
	private := ed25519.NewKeyFromSeed(seed[:])
	public := private.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	pin := hex.EncodeToString(digest[:])
	path := filepath.Join(t.TempDir(), "person.seed")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(seed[:])), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := LoadProcessPersonOriginKey(context.Background(), path, pin)
	if err != nil || string(key.Public().(ed25519.PublicKey)) != string(public) {
		t.Fatal("pinned process key unavailable", err)
	}
	if key, err := LoadProcessPersonOriginKey(context.Background(), path,
		rawSHA([]byte("foreign"))); err == nil || key != nil {
		t.Fatal("foreign public pin accepted")
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if key, err := LoadProcessPersonOriginKey(context.Background(), path, pin); err == nil || key != nil {
		t.Fatal("group-readable secret accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(path), "person.link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if key, err := LoadProcessPersonOriginKey(context.Background(), link, pin); err == nil || key != nil {
		t.Fatal("linked secret accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if key, err := LoadProcessPersonOriginKey(ctx, path, pin); err != context.Canceled || key != nil {
		t.Fatal("cancelled secret read proceeded", err)
	}
}
