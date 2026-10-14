package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type importNativeStyleDeploymentWire struct {
	Schema          string            `json:"schema_version"`
	WorkICCSHA      string            `json:"work_icc_sha256"`
	StyleRuntimeSHA string            `json:"style_runtime_sha256"`
	ImageRuntimeSHA string            `json:"image_runtime_sha256"`
	Models          map[string]string `json:"models"`
}

// DecodeImportNativeStyleInstallation accepts only exact, pinned operator
// bytes. Archive metadata and task Params cannot install ICC or model pins.
func DecodeImportNativeStyleInstallation(raw []byte, pin string) (ImportNativeStyleInstallation, error) {
	var zero ImportNativeStyleInstallation
	if len(raw) < 2 || len(raw) > 1<<20 || !validImportRefForSHA(pin) || rawSHA(raw) != pin {
		return zero, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return zero, v2input.ErrInvalid
	}
	var wire importNativeStyleDeploymentWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return zero, err
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(exact, raw) || wire.Schema != "interchange-import-style-installation/v1" ||
		!validImportRefForSHA(wire.WorkICCSHA) || !validImportRefForSHA(wire.StyleRuntimeSHA) || !validImportRefForSHA(wire.ImageRuntimeSHA) || wire.Models == nil || len(wire.Models) > 1024 {
		return zero, v2input.ErrInvalid
	}
	for name, lock := range wire.Models {
		if name == "" || len(name) > 240 || !validImportRefForSHA(lock) {
			return zero, v2input.ErrInvalid
		}
	}
	return ImportNativeStyleInstallation{WorkICCSHA: wire.WorkICCSHA, StyleRuntimeSHA: wire.StyleRuntimeSHA, ImageRuntimeSHA: wire.ImageRuntimeSHA, Models: wire.Models}.owned(), nil
}

// LoadImportNativeStyleInstallation refuses symlinks, writable shared files,
// and replacement or mutation while reading the exact pinned bytes.
func LoadImportNativeStyleInstallation(ctx context.Context, path, pin string) (ImportNativeStyleInstallation, error) {
	var zero ImportNativeStyleInstallation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !filepath.IsAbs(path) || !validImportRefForSHA(pin) {
		return zero, v2input.ErrInvalid
	}
	before, err := os.Lstat(path)
	if err != nil {
		return zero, err
	}
	if !singleLinkImportOperatorFile(before) || !before.Mode().IsRegular() || before.Mode().Perm()&0022 != 0 || before.Size() < 2 || before.Size() > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return zero, err
	}
	if !singleLinkImportOperatorFile(opened) || !os.SameFile(before, opened) || opened.Size() != before.Size() || opened.Mode() != before.Mode() || !opened.ModTime().Equal(before.ModTime()) {
		return zero, v2input.ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(exportContextReader{ctx, file}, (1<<20)+1))
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	after, err := file.Stat()
	if err != nil {
		return zero, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return zero, err
	}
	if !singleLinkImportOperatorFile(after) || !singleLinkImportOperatorFile(current) || !os.SameFile(opened, after) || !os.SameFile(after, current) || after.Size() != opened.Size() || after.Mode() != opened.Mode() || !after.ModTime().Equal(opened.ModTime()) || current.Mode() != opened.Mode() || current.Size() != opened.Size() || !current.ModTime().Equal(opened.ModTime()) || int64(len(raw)) != opened.Size() {
		return zero, v2input.ErrInvalid
	}
	return DecodeImportNativeStyleInstallation(raw, pin)
}
