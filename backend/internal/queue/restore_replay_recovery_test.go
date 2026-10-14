package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestReplayRecoveryReportBinding(t *testing.T) {
	for _, mode := range []string{"valid", "stale_pin", "foreign_run", "foreign_database", "foreign_build", "missing_stage", "writer_approval", "noncanonical", "symlink", "public_file", "changed_after_read", "bad_time", "reversed_time", "future_time", "bad_lsn", "missing_time", "negative_pending", "count_mismatch", "replay_before_recovery"} {
		t.Run(mode, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			installation := RestoreReplayReceiptInstallation{Root: root, RunID: uuid.New(), BuildSHA256: strings.Repeat("b", 64), RecoveryReportPath: filepath.Join(root, "recovery.json")}
			recovery := map[string]any{"schema_version": "restore-compute-recovery/v1", "preflight": map[string]any{"schema_version": "restore-preflight/v1", "run_id": installation.RunID.String(), "deletions": map[string]any{"database": "isolated"}, "storage": map[string]any{"database": map[string]any{"database": "isolated"}}}, "consent": map[string]any{"schema_version": "restore-consent-verification/v1"}, "deletion_checkpoint": map[string]any{"sequence": 0}, "quarantine": map[string]any{"schema_version": "restore-quarantine-activation/v1", "run_id": installation.RunID.String()}}
			stages := map[string]string{"leases": "restore-compute-leases/v1", "resources": "restore-resource-recovery/v1", "scheduler_grants": "restore-scheduler-grants/v1", "cleanup": "restore-cleanup-fencing/v1", "intents": "restore-expired-intents/v1", "mail": "restore-mail-recovery/v1", "outbox": "restore-outbox-preparation/v1", "keys": "restore-key-versions/v1"}
			for key, schema := range stages {
				recovery[key] = map[string]any{"schema_version": schema, "database": "isolated"}
			}
			recovery["outbox"].(map[string]any)["pending_task_events"] = int64(205)
			recovery["outbox"].(map[string]any)["pending_task_sha256"] = strings.Repeat("a", 64)
			document := map[string]any{"schema_version": "restore-recovery-report/v1", "writers_authorized": false, "build_sha256": installation.BuildSHA256, "backup_at": "2020-10-11T00:00:00+00:00", "started_at": "2020-10-11T01:00:00+00:00", "finished_at": "2020-10-11T02:00:00+00:00", "last_wal_lsn": "0/ABC", "recovery": recovery}
			switch mode {
			case "foreign_run":
				recovery["preflight"].(map[string]any)["run_id"] = uuid.New().String()
			case "foreign_database":
				recovery["keys"].(map[string]any)["database"] = "production"
			case "foreign_build":
				document["build_sha256"] = strings.Repeat("c", 64)
			case "missing_stage":
				delete(recovery, "resources")
			case "bad_time":
				document["finished_at"] = "invalid"
			case "reversed_time":
				document["backup_at"] = "2020-10-12T00:00:00Z"
			case "future_time":
				document["finished_at"] = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
			case "bad_lsn":
				document["last_wal_lsn"] = "0/abc"
			case "missing_time":
				delete(document, "started_at")
				document["unrelated"] = true
			case "negative_pending":
				recovery["outbox"].(map[string]any)["pending_task_events"] = -1
			case "writer_approval":
				document["writers_authorized"] = true
			}
			raw, err := canonical.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "noncanonical" {
				raw = append(raw, '\n')
			}
			sum := sha256.Sum256(raw)
			installation.RecoverySHA256 = hex.EncodeToString(sum[:])
			if mode == "stale_pin" {
				installation.RecoverySHA256 = strings.Repeat("a", 64)
			}
			if err = os.WriteFile(installation.RecoveryReportPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "public_file" {
				if err = os.Chmod(installation.RecoveryReportPath, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "symlink" {
				target := installation.RecoveryReportPath
				installation.RecoveryReportPath = filepath.Join(root, "link.json")
				if err = os.Symlink(target, installation.RecoveryReportPath); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "changed_after_read" {
				if err = verifyReplayRecoveryReport(context.Background(), installation, "isolated"); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(installation.RecoveryReportPath, append(raw, ' '), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "count_mismatch" || mode == "replay_before_recovery" {
				evidence := RestoreReplayEvidence{Database: "isolated", InitiallyPending: 205, Confirmed: 205, CompletedAt: time.Now().UTC()}
				if mode == "count_mismatch" {
					evidence.InitiallyPending = 204
					evidence.Confirmed = 204
				} else {
					evidence.CompletedAt = time.Date(2020, 10, 10, 0, 0, 0, 0, time.UTC)
				}
				err = verifyReplayRecoveryReport(context.Background(), installation, "isolated", evidence)
			} else {
				err = verifyReplayRecoveryReport(context.Background(), installation, "isolated")
			}
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("accepted invalid recovery binding")
			}
		})
	}
}
