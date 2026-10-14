package interchange

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"photoeditor/backend/internal/v2input"
)

// LoadNativeImportDeployment reads only an operator-supplied, pinned regular
// file. The same inode, mode, size and mtime must survive the bounded read.
// Native execution and database access belong to Build, after full startup
// installation has validated every other required dependency.
func LoadNativeImportDeployment(ctx context.Context, path, pin string) (NativeImportDeployment, error) {
	var zero NativeImportDeployment
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
	if !before.Mode().IsRegular() || before.Mode().Perm()&0022 != 0 || before.Size() < 2 || before.Size() > 9<<20 {
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
	if !os.SameFile(before, opened) || opened.Size() != before.Size() || opened.Mode() != before.Mode() || !opened.ModTime().Equal(before.ModTime()) {
		return zero, v2input.ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(exportContextReader{ctx, file}, (9<<20)+1))
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
	if !os.SameFile(opened, after) || !os.SameFile(after, current) || after.Size() != opened.Size() || after.Mode() != opened.Mode() || !after.ModTime().Equal(opened.ModTime()) || current.Mode() != opened.Mode() || current.Size() != opened.Size() || !current.ModTime().Equal(opened.ModTime()) || int64(len(raw)) != opened.Size() {
		return zero, v2input.ErrInvalid
	}
	return DecodeNativeImportDeployment(raw, pin)
}
