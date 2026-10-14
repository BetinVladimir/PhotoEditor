package interchange

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/v2input"
)

// Read a pinned process configuration, never a path taken from a job request.
// No native code, package import or object-storage operation occurs here.
func LoadImportPersonMaskQualification(ctx context.Context, path, pin string, consent ImportMaskQualifier) (ImportPersonMaskQualificationInstallation, error) {
	var zero ImportPersonMaskQualificationInstallation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if consent == nil {
		return zero, v2input.ErrInvalid
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
	return DecodeImportPersonMaskQualification(ctx, raw, pin, consent)
}
