package queue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"photoeditor/backend/internal/canonical"
)

// verifyReplayRecoveryReport binds replay to private, externally pinned recovery
// evidence. It does not attest independent authorities or authorize writers.
func verifyReplayRecoveryReport(ctx context.Context, installation RestoreReplayReceiptInstallation, database string, replay ...RestoreReplayEvidence) error {
	return verifyReplayRecoveryReportBound(ctx, installation, database, nil, nil, replay...)
}

func verifyReplayRecoveryReportBound(ctx context.Context, installation RestoreReplayReceiptInstallation, database string, expected *int64, expectedSHA *string, replay ...RestoreReplayEvidence) error {
	refuse := errors.New("restore replay recovery report invalid")
	if err := ctx.Err(); err != nil {
		return err
	}
	path := installation.RecoveryReportPath
	if database == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return refuse
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return refuse
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return refuse
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 || before.Size() <= 0 || before.Size() > 8<<20 {
		return refuse
	}
	owner, ok := before.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Getuid()) || owner.Nlink != 1 {
		return refuse
	}
	raw, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil || int64(len(raw)) != before.Size() {
		return refuse
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return refuse
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, current) || current.Mode() != before.Mode() || current.Size() != before.Size() || !current.ModTime().Equal(before.ModTime()) {
		return refuse
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != installation.RecoverySHA256 {
		return refuse
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, normalized) {
		return refuse
	}
	var document map[string]json.RawMessage
	if json.Unmarshal(raw, &document) != nil || len(document) != 8 {
		return refuse
	}
	stringValue := func(m map[string]json.RawMessage, key string) string {
		var s string
		_ = json.Unmarshal(m[key], &s)
		return s
	}
	object := func(m map[string]json.RawMessage, key string) map[string]json.RawMessage {
		var v map[string]json.RawMessage
		_ = json.Unmarshal(m[key], &v)
		return v
	}
	if stringValue(document, "schema_version") != "restore-recovery-report/v1" || stringValue(document, "build_sha256") != installation.BuildSHA256 || string(document["writers_authorized"]) != "false" {
		return refuse
	}

	// Closed top-level fields prevent a missing timestamp being replaced by an
	// unrelated member while preserving the same object size.
	for _, key := range []string{"schema_version", "writers_authorized", "build_sha256", "backup_at", "last_wal_lsn", "started_at", "finished_at", "recovery"} {
		if _, exists := document[key]; !exists {
			return refuse
		}
	}
	backup, backupErr := time.Parse(time.RFC3339Nano, stringValue(document, "backup_at"))
	started, startedErr := time.Parse(time.RFC3339Nano, stringValue(document, "started_at"))
	finished, finishedErr := time.Parse(time.RFC3339Nano, stringValue(document, "finished_at"))
	if backupErr != nil || startedErr != nil || finishedErr != nil || backup.IsZero() || started.IsZero() || finished.IsZero() || backup.After(started) || started.After(finished) || finished.After(time.Now()) || !regexp.MustCompile(`^[0-9A-F]{1,8}/[0-9A-F]{1,8}$`).MatchString(stringValue(document, "last_wal_lsn")) {
		return refuse
	}
	recovery := object(document, "recovery")
	preflight := object(recovery, "preflight")
	if stringValue(recovery, "schema_version") != "restore-compute-recovery/v1" || stringValue(preflight, "schema_version") != "restore-preflight/v1" || stringValue(preflight, "run_id") != installation.RunID.String() {
		return refuse
	}
	if stringValue(object(preflight, "deletions"), "database") != database || stringValue(object(object(preflight, "storage"), "database"), "database") != database {
		return refuse
	}
	stages := map[string]string{"leases": "restore-compute-leases/v1", "resources": "restore-resource-recovery/v1", "scheduler_grants": "restore-scheduler-grants/v1", "cleanup": "restore-cleanup-fencing/v1", "intents": "restore-expired-intents/v1", "mail": "restore-mail-recovery/v1", "outbox": "restore-outbox-preparation/v1", "keys": "restore-key-versions/v1"}
	for name, schema := range stages {
		stage := object(recovery, name)
		if stringValue(stage, "schema_version") != schema || stringValue(stage, "database") != database {
			return refuse
		}
	}
	quarantine := object(recovery, "quarantine")
	if stringValue(quarantine, "schema_version") != "restore-quarantine-activation/v1" || stringValue(quarantine, "run_id") != installation.RunID.String() || stringValue(object(recovery, "consent"), "schema_version") != "restore-consent-verification/v1" || object(recovery, "deletion_checkpoint") == nil {
		return refuse
	}

	var pending int64
	if json.Unmarshal(object(recovery, "outbox")["pending_task_events"], &pending) != nil || pending < 0 || pending > 10_000_000 {
		return refuse
	}
	sha := stringValue(object(recovery, "outbox"), "pending_task_sha256")
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(sha) {
		return refuse
	}
	if len(replay) > 1 {
		return refuse
	}
	if len(replay) == 1 {
		evidence := replay[0]
		if evidence.Database != database || evidence.InitiallyPending != pending || evidence.Confirmed != pending || evidence.CompletedAt.IsZero() || evidence.CompletedAt.Before(finished) {
			return refuse
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if expectedSHA != nil {
		*expectedSHA = sha
	}
	if expected != nil {
		*expected = pending
	}
	return nil
}
