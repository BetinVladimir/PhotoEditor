package maintenance

import (
	"context"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
	"time"
)

func TestDeletionHistoryAcknowledgementExactScope(t *testing.T) {
	for _, mode := range []string{"valid", "event", "tenant", "sequence", "head", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			event, tenant := uuid.New(), uuid.New()
			wire := map[string]any{"schema_version": 1, "event_id": event, "tenant_id": tenant, "sequence": int64(1), "head_sha256": strings.Repeat("a", 64)}
			switch mode {
			case "event":
				wire["event_id"] = uuid.New()
			case "tenant":
				wire["tenant_id"] = uuid.New()
			case "sequence":
				wire["sequence"] = 0
			case "head":
				wire["head_sha256"] = "bad"
			case "unknown":
				wire["approved"] = true
			}
			raw, err := canonical.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if err = validateDeletionHistoryAcknowledgement(raw, event, tenant); (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
		})
	}
}
func TestDeletionHistoryAppenderCancellationBeforeProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (PythonTenantDeletionHistoryAppender{}).AppendTenantDeletionRequested(ctx, uuid.New(), uuid.New(), time.Now())
	if err != context.Canceled {
		t.Fatal("cancelled append started installation", err)
	}
}

func TestDeletionHistoryRootRemainsPrivateAtAppendTime(t *testing.T) {
	root := filepath.Join(t.TempDir(), "history")
	if err := os.Mkdir(root, 0700); err != nil { t.Fatal(err) }
	if !privateDeletionHistoryRoot(root) { t.Fatal("private history root rejected") }
	if err := os.Chmod(root, 0750); err != nil { t.Fatal(err) }
	if privateDeletionHistoryRoot(root) { t.Fatal("readable history root accepted") }
	if err := os.Chmod(root, 0700); err != nil { t.Fatal(err) }
	alias := filepath.Join(t.TempDir(), "history-link")
	if err := os.Symlink(root, alias); err != nil { t.Fatal(err) }
	if privateDeletionHistoryRoot(alias) { t.Fatal("symlink history root accepted") }
	original, err := os.Lstat(root)
	if err != nil { t.Fatal(err) }
	if !unchangedPrivateDeletionHistoryRoot(root, original) { t.Fatal("stable history root rejected") }
	old := root + "-old"
	if err := os.Rename(root, old); err != nil { t.Fatal(err) }
	if err := os.Mkdir(root, 0700); err != nil { t.Fatal(err) }
	if unchangedPrivateDeletionHistoryRoot(root, original) { t.Fatal("replacement history root accepted") }
}
