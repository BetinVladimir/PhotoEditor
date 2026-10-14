package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRestoreReplayReceiptPrivateExclusiveAndNoWriterApproval(t *testing.T) {
	root, err := os.MkdirTemp("", "restore-receipt-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	installation := RestoreReplayReceiptInstallation{Root: root, RunID: uuid.New(), RecoverySHA256: strings.Repeat("a", 64), BuildSHA256: strings.Repeat("b", 64)}
	evidence := RestoreReplayEvidence{Database: "isolated", InitiallyPending: 205,
		Confirmed: 205, CompletedAt: time.Now().UTC(), sentInventorySHA: strings.Repeat("c", 64),
		boundRecoverySHA: installation.RecoverySHA256, boundBuildSHA: installation.BuildSHA256,
		boundRunID: installation.RunID}
	evidence.attested, evidence.attestedDatabase, evidence.attestedCount, evidence.attestedAt =
		true, evidence.Database, evidence.Confirmed, evidence.CompletedAt
	receipt, err := PersistRestoreReplayReceipt(context.Background(), installation, evidence)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(receipt.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if receipt.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("digest mismatch")
	}
	var document map[string]any
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document["writers_authorized"] != false || document["recovery_sha256"] != installation.RecoverySHA256 {
		t.Fatal("unbound or writer approval")
	}
	info, err := os.Stat(receipt.Path)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("receipt not private")
	}
	if _, err = PersistRestoreReplayReceipt(context.Background(), installation, evidence); err == nil {
		t.Fatal("overwrote receipt")
	}
	after, _ := os.ReadFile(receipt.Path)
	if string(after) != string(raw) {
		t.Fatal("changed immutable receipt")
	}
}

func TestRestoreReplayReceiptRefusesIncompleteAndCancelled(t *testing.T) {
	for _, mode := range []string{"partial", "cancelled", "bad_pin", "unverified", "foreign_run", "foreign_build", "altered_counts", "altered_time"} {
		t.Run(mode, func(t *testing.T) {
			root, err := os.MkdirTemp("", "restore-receipt-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			root, err = filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			installation := RestoreReplayReceiptInstallation{Root: root, RunID: uuid.New(), RecoverySHA256: strings.Repeat("a", 64), BuildSHA256: strings.Repeat("b", 64)}
			evidence := RestoreReplayEvidence{Database: "isolated", InitiallyPending: 3,
				Confirmed: 3, CompletedAt: time.Now().UTC(), sentInventorySHA: strings.Repeat("c", 64),
				boundRecoverySHA: installation.RecoverySHA256, boundBuildSHA: installation.BuildSHA256,
				boundRunID: installation.RunID}
			evidence.attested, evidence.attestedDatabase, evidence.attestedCount, evidence.attestedAt =
				true, evidence.Database, evidence.Confirmed, evidence.CompletedAt
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "partial" {
				evidence.Confirmed = 2
			}
			if mode == "cancelled" {
				cancel()
			}
			if mode == "bad_pin" {
				installation.RecoverySHA256 = "bad"
			}
			if mode == "unverified" {
				evidence.sentInventorySHA = ""
			}
			if mode == "foreign_run" {
				evidence.boundRunID = uuid.New()
			}
			if mode == "foreign_build" {
				evidence.boundBuildSHA = strings.Repeat("d", 64)
			}
			if mode == "altered_counts" {
				evidence.InitiallyPending, evidence.Confirmed = 4, 4
			}
			if mode == "altered_time" {
				evidence.CompletedAt = evidence.CompletedAt.Add(time.Second)
			}
			if _, err = PersistRestoreReplayReceipt(ctx, installation, evidence); err == nil {
				t.Fatal("invalid success receipt")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("partial report published")
			}
		})
	}
}
