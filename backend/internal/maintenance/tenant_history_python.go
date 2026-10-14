package maintenance

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// Python must be a process-qualified isolated environment with the worker wheel
// installed. Root is independent operator storage, excluded from DB restore.
type PythonTenantDeletionHistoryAppender struct {
	Python, Root string
	Timeout      time.Duration
}

func (s PythonTenantDeletionHistoryAppender) AppendTenantDeletionRequested(ctx context.Context, event, tenant uuid.UUID, occurred time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if event == uuid.Nil || tenant == uuid.Nil || occurred.IsZero() || s.Timeout < time.Second || s.Timeout > 3*time.Second || !filepath.IsAbs(s.Python) || filepath.Clean(s.Python) != s.Python || !filepath.IsAbs(s.Root) || filepath.Clean(s.Root) != s.Root {
		return errors.New("deletion history installation invalid")
	}
	if !privateDeletionHistoryRoot(s.Root) {
		return errors.New("deletion history directory unavailable")
	}
	rootIdentity, err := os.Lstat(s.Root)
	if err != nil || !rootIdentity.IsDir() { return errors.New("deletion history directory unavailable") }
	fd, err := syscall.Open(s.Root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil { return errors.New("deletion history directory unavailable") }
	rootFile := os.NewFile(uintptr(fd), s.Root)
	defer rootFile.Close()
	opened, err := rootFile.Stat()
	if err != nil || !os.SameFile(rootIdentity, opened) || !privateDeletionHistoryRoot(s.Root) {
		return errors.New("deletion history directory changed")
	}
	raw, err := canonical.Marshal(struct {
		Schema int       `json:"schema_version"`
		Event  uuid.UUID `json:"event_id"`
		Tenant uuid.UUID `json:"tenant_id"`
		At     string    `json:"occurred_at"`
	}{1, event, tenant, occurred.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	scoped, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	command := exec.CommandContext(scoped, s.Python, "-I", "-m", "photo_worker.runtime.deletion_history_cli", "--root-fd", "3")
	command.ExtraFiles = []*os.File{rootFile}
	command.Stdin = bytes.NewReader(raw)
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	output := &historyOutput{maximum: 4096}
	command.Stdout = output
	if err = command.Run(); err != nil {
		if scoped.Err() != nil {
			return scoped.Err()
		}
		return errors.New("independent deletion history unavailable")
	}
	if err = scoped.Err(); err != nil {
		return err
	}
	if !unchangedPrivateDeletionHistoryRoot(s.Root, rootIdentity) {
		return errors.New("deletion history directory changed")
	}
	return validateDeletionHistoryAcknowledgement(output.Bytes(), event, tenant)
}

type historyOutput struct {
	bytes.Buffer
	maximum int
}

func (b *historyOutput) Write(raw []byte) (int, error) {
	if len(raw) > b.maximum-b.Len() {
		return 0, errors.New("deletion history response too large")
	}
	return b.Buffer.Write(raw)
}
func validateDeletionHistoryAcknowledgement(raw []byte, event, tenant uuid.UUID) error {
	invalid := func() error { return errors.New("independent deletion history acknowledgement invalid") }
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return invalid()
	}
	var response struct {
		Schema   int       `json:"schema_version"`
		Event    uuid.UUID `json:"event_id"`
		Tenant   uuid.UUID `json:"tenant_id"`
		Sequence int64     `json:"sequence"`
		Head     string    `json:"head_sha256"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&response) != nil || response.Schema != 1 || response.Event != event || response.Tenant != tenant || response.Sequence < 1 || response.Sequence > 9007199254740991 {
		return invalid()
	}
	decoded, err := hex.DecodeString(response.Head)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != response.Head {
		return invalid()
	}
	return nil
}
