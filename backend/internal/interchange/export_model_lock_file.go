package interchange

import (
	"bytes"
	"context"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// LoadExportModelLockSHA proves that the deployment's model lock is a pinned,
// local canonical document, independently of the export operator manifest.
func LoadExportModelLockSHA(ctx context.Context, path, pin string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validImportRefForSHA(pin) {
		return "", v2input.ErrInvalid
	}
	raw, err := readExportPolicyFile(ctx, path, 1<<20)
	if err != nil {
		return "", err
	}
	if rawSHA(raw) != pin {
		return "", v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return "", v2input.ErrInvalid
	}
	return pin, ctx.Err()
}
