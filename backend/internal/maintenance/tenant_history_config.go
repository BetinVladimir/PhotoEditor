package maintenance

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func privateDeletionHistoryRoot(root string) bool {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 { return false }
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && secureTenantPurgeOwner(stat.Uid, uint32(os.Geteuid()))
}

func unchangedPrivateDeletionHistoryRoot(root string, original os.FileInfo) bool {
	if original == nil || !original.IsDir() || !privateDeletionHistoryRoot(root) { return false }
	current, err := os.Lstat(root)
	return err == nil && os.SameFile(original, current)
}

func loadTenantDeletionHistoryAppender() (TenantDeletionHistoryAppender, bool, error) {
	python, root, required := os.Getenv("V32_DELETION_HISTORY_PYTHON"), os.Getenv("V32_DELETION_HISTORY_ROOT"), os.Getenv("V32_DELETION_HISTORY_REQUIRED")
	if required != "" && required != "true" && required != "false" {
		return nil, false, errors.New("invalid deletion history required flag")
	}
	if python == "" && root == "" {
		if required == "true" {
			return nil, true, errors.New("independent deletion history required")
		}
		return nil, false, nil
	}
	if !filepath.IsAbs(python) || filepath.Clean(python) != python || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, true, errors.New("independent deletion history requires absolute installed paths")
	}
	if !privateDeletionHistoryRoot(root) {
		return nil, true, errors.New("independent deletion history private directory unavailable")
	}
	executable, err := os.Stat(python)
	if err != nil || !executable.Mode().IsRegular() || executable.Mode().Perm()&0111 == 0 || executable.Mode().Perm()&0022 != 0 {
		return nil, true, errors.New("independent deletion history Python unavailable")
	}
	return PythonTenantDeletionHistoryAppender{Python: python, Root: root, Timeout: 3 * time.Second}, true, nil
}
