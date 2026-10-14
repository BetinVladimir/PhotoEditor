package interchange

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"photoeditor/backend/internal/v2input"
)

// readExportPolicyFile rejects path replacement, links and blocking special
// files before decoding an operator-owned qualification policy. The policy
// itself still has to pass its operation-specific schema and lock checks.
func readExportPolicyFile(ctx context.Context, path string, limit int64) ([]byte, error) {
	return readExportOperatorFile(ctx, path, limit, false)
}

func readExportSecretFile(ctx context.Context, path string, limit int64) ([]byte, error) {
	return readExportOperatorFile(ctx, path, limit, true)
}

func readExportOperatorFile(ctx context.Context, path string, limit int64, secret bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || limit < 2 {
		return nil, v2input.ErrInvalid
	}
	acceptable := func(info os.FileInfo) bool {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 ||
			(secret && info.Mode().Perm()&0077 != 0) ||
			info.Size() < 2 || info.Size() > limit {
			return false
		}
		identity, ok := info.Sys().(*syscall.Stat_t)
		return ok && identity.Nlink == 1
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !acceptable(before) {
		return nil, v2input.ErrInvalid
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !acceptable(opened) || !sameExportPolicyFile(before, opened) {
		return nil, v2input.ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(exportContextReader{ctx, file}, limit+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !acceptable(after) || !acceptable(current) ||
		!sameExportPolicyFile(opened, after) || !sameExportPolicyFile(after, current) ||
		int64(len(raw)) != opened.Size() {
		return nil, v2input.ErrInvalid
	}
	return raw, nil
}

func sameExportPolicyFile(left, right os.FileInfo) bool {
	return os.SameFile(left, right) && left.Size() == right.Size() &&
		left.Mode() == right.Mode() && left.ModTime().Equal(right.ModTime())
}
