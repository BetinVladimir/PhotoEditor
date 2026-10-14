package interchange

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/v2input"
	"syscall"
)

// Read a pinned process configuration, never a path taken from a job request.
// No native code, package import or object-storage operation occurs here.
func LoadNativeExportDeployment(ctx context.Context, path, pin string) (NativeExportDeployment, error) {
	var zero NativeExportDeployment
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !validImportRefForSHA(pin) {
		return zero, v2input.ErrInvalid
	}
	acceptable := func(info os.FileInfo) bool {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() < 2 || info.Size() > 9<<20 {
			return false
		}
		identity, ok := info.Sys().(*syscall.Stat_t)
		return ok && identity.Nlink == 1
	}
	before, err := os.Lstat(path)
	if err != nil {
		return zero, err
	}
	if !acceptable(before) {
		return zero, v2input.ErrInvalid
	}
	// A concurrent path replacement cannot turn this read into a blocking FIFO
	// or cause the loader to follow a newly installed symlink.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return zero, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return zero, err
	}
	if !acceptable(opened) || !os.SameFile(before, opened) || opened.Size() != before.Size() || opened.Mode() != before.Mode() || !opened.ModTime().Equal(before.ModTime()) {
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
	if !acceptable(after) || !acceptable(current) || !os.SameFile(opened, after) || !os.SameFile(after, current) || after.Size() != opened.Size() || after.Mode() != opened.Mode() || !after.ModTime().Equal(opened.ModTime()) || current.Mode() != opened.Mode() || current.Size() != opened.Size() || !current.ModTime().Equal(opened.ModTime()) || int64(len(raw)) != opened.Size() {
		return zero, v2input.ErrInvalid
	}
	return DecodeNativeExportDeployment(raw, pin)
}
