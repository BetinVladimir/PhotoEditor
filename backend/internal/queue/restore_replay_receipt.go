package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type RestoreReplayReceiptInstallation struct {
	RecoveryReportPath string
	Root               string
	RunID              uuid.UUID
	RecoverySHA256     string
	BuildSHA256        string
}

type RestoreReplayReceipt struct {
	Path   string
	SHA256 string
}

func validateRestoreReplayReceiptInstallation(ctx context.Context, installation RestoreReplayReceiptInstallation) error {
	pin := regexp.MustCompile(`^[0-9a-f]{64}$`)
	if !filepath.IsAbs(installation.Root) || filepath.Clean(installation.Root) != installation.Root || installation.RunID == uuid.Nil || !pin.MatchString(installation.RecoverySHA256) || !pin.MatchString(installation.BuildSHA256) {
		return errors.New("restore replay receipt installation invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(installation.Root)
	if err != nil || resolved != installation.Root {
		return errors.New("restore replay receipt root invalid")
	}
	info, err := os.Stat(installation.Root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return errors.New("restore replay receipt root permissions")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Getuid()) {
		return errors.New("restore replay receipt root owner")
	}
	return nil
}

func PersistRestoreReplayReceipt(ctx context.Context, installation RestoreReplayReceiptInstallation, evidence RestoreReplayEvidence) (RestoreReplayReceipt, error) {
	empty := RestoreReplayReceipt{}
	if err := validateRestoreReplayReceiptInstallation(ctx, installation); err != nil {
		return empty, err
	}
	if evidence.Database == "" || evidence.CompletedAt.IsZero() || evidence.InitiallyPending < 0 || evidence.InitiallyPending > 10_000_000 || evidence.InitiallyPending != evidence.Confirmed || !evidence.attested || evidence.attestedDatabase != evidence.Database || evidence.attestedCount != evidence.Confirmed || !evidence.attestedAt.Equal(evidence.CompletedAt) || evidence.boundRunID != installation.RunID || evidence.boundRecoverySHA != installation.RecoverySHA256 || evidence.boundBuildSHA != installation.BuildSHA256 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(evidence.sentInventorySHA) {
		return empty, errors.New("restore replay evidence invalid")
	}
	raw, err := canonical.Marshal(map[string]any{
		"schema_version": "restore-task-replay-receipt/v1", "run_id": installation.RunID.String(),
		"recovery_sha256": installation.RecoverySHA256, "build_sha256": installation.BuildSHA256,
		"database": evidence.Database, "initially_pending": evidence.InitiallyPending, "confirmed": evidence.Confirmed,
		"completed_at": evidence.CompletedAt.UTC().Format(time.RFC3339Nano), "writers_authorized": false,
	})
	if err != nil {
		return empty, errors.New("restore replay receipt encoding failed")
	}
	pending := filepath.Join(installation.Root, installation.RunID.String()+".replay.pending")
	final := filepath.Join(installation.Root, installation.RunID.String()+".replay.json")
	file, err := os.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return empty, err
	}
	defer os.Remove(pending)
	n, writeErr := file.Write(raw)
	if writeErr == nil && n != len(raw) {
		writeErr = errors.New("restore replay receipt short write")
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return empty, writeErr
	}
	if closeErr != nil {
		return empty, closeErr
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if err = os.Link(pending, final); err != nil {
		return empty, err
	}
	if err = os.Remove(pending); err != nil {
		return empty, err
	}
	directory, err := os.Open(installation.Root)
	if err != nil {
		return empty, err
	}
	syncErr := directory.Sync()
	closeErr = directory.Close()
	if syncErr != nil {
		return empty, syncErr
	}
	if closeErr != nil {
		return empty, closeErr
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	sum := sha256.Sum256(raw)
	return RestoreReplayReceipt{final, hex.EncodeToString(sum[:])}, nil
}

func RunRestoreReplayAndReport(ctx context.Context, dsn, brokerURL string,
	installation RestoreReplayInstallation, receipt RestoreReplayReceiptInstallation) (RestoreReplayEvidence, RestoreReplayReceipt, error) {
	if err := validateRestoreReplayReceiptInstallation(ctx, receipt); err != nil {
		return RestoreReplayEvidence{}, RestoreReplayReceipt{}, err
	}
	var expected int64
	var expectedSHA string
	if err := verifyReplayRecoveryReportBound(ctx, receipt, installation.Database, &expected, &expectedSHA); err != nil {
		return RestoreReplayEvidence{}, RestoreReplayReceipt{}, err
	}
	evidence, err := runRestoreReplay(ctx, dsn, brokerURL, installation, &expected, expectedSHA)
	if err != nil {
		return RestoreReplayEvidence{}, RestoreReplayReceipt{}, err
	}
	if err := verifyReplayRecoveryReport(ctx, receipt, evidence.Database, evidence); err != nil {
		return RestoreReplayEvidence{}, RestoreReplayReceipt{}, err
	}
	if evidence.sentInventorySHA != expectedSHA {
		return RestoreReplayEvidence{}, RestoreReplayReceipt{}, errors.New("restore replay sent inventory unbound")
	}
	evidence.boundRecoverySHA = receipt.RecoverySHA256
	evidence.boundBuildSHA = receipt.BuildSHA256
	evidence.boundRunID = receipt.RunID
	stored, err := PersistRestoreReplayReceipt(ctx, receipt, evidence)
	if err != nil {
		return RestoreReplayEvidence{}, RestoreReplayReceipt{}, err
	}
	return evidence, stored, nil
}
