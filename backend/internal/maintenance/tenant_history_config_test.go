package maintenance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTenantDeletionHistoryStartupConfiguration(t *testing.T) {
	for _, mode := range []string{"disabled", "valid", "required_missing", "partial", "relative", "invalid_flag", "shared_root", "symlink_root"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("V32_DELETION_HISTORY_REQUIRED", "")
			t.Setenv("V32_DELETION_HISTORY_PYTHON", "")
			t.Setenv("V32_DELETION_HISTORY_ROOT", "")
			if mode == "valid" || mode == "shared_root" || mode == "symlink_root" {
				root := t.TempDir()
				python := filepath.Join(t.TempDir(), "python")
				if err := os.WriteFile(python, []byte("operator-installed fixture"), 0700); err != nil {
					t.Fatal(err)
				}
				if mode == "shared_root" {
					if err := os.Chmod(root, 0777); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "symlink_root" {
					link := filepath.Join(t.TempDir(), "history-link")
					if err := os.Symlink(root, link); err != nil { t.Fatal(err) }
					root = link
				}
				t.Setenv("V32_DELETION_HISTORY_PYTHON", python)
				t.Setenv("V32_DELETION_HISTORY_ROOT", root)
			}
			switch mode {
			case "required_missing":
				t.Setenv("V32_DELETION_HISTORY_REQUIRED", "true")
			case "partial":
				t.Setenv("V32_DELETION_HISTORY_ROOT", "/private/history")
			case "relative":
				t.Setenv("V32_DELETION_HISTORY_PYTHON", "python")
			case "invalid_flag":
				t.Setenv("V32_DELETION_HISTORY_REQUIRED", "yes")
			}
			appender, required, err := loadTenantDeletionHistoryAppender()
			if mode == "disabled" {
				if err != nil || appender != nil || required {
					t.Fatal("legacy disabled startup changed")
				}
				return
			}
			if mode == "valid" {
				if err != nil || appender == nil || !required {
					t.Fatal("installed journal unavailable", err)
				}
				return
			}
			if err == nil || appender != nil {
				t.Fatal("invalid startup accepted")
			}
		})
	}
}
