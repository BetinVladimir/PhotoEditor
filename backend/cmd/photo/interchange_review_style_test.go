package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func TestInterchangeReviewStyleDisabled(t *testing.T) {
	t.Setenv("PHOTO_V30_IMPORT_STYLE_CONFIG", "")
	t.Setenv("PHOTO_V30_IMPORT_STYLE_CONFIG_SHA256", "")
	factory, err := buildInterchangeReviewStyle("")
	if err != nil || factory != nil {
		t.Fatal("unconfigured factory must remain disabled")
	}
}
func TestInterchangeReviewStyleIncompleteConfiguration(t *testing.T) {
	for _, tc := range []struct{ path, pin string }{
		{"/missing/config.json", ""}, {"", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"relative.json", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"/missing/config.json", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	} {
		t.Run(tc.path+tc.pin, func(t *testing.T) {
			t.Setenv("PHOTO_V30_IMPORT_STYLE_CONFIG", tc.path)
			t.Setenv("PHOTO_V30_IMPORT_STYLE_CONFIG_SHA256", tc.pin)
			factory, err := buildInterchangeReviewStyle("")
			if err == nil || factory != nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestInterchangeReviewStylePinnedConfiguration(t *testing.T) {
	root := t.TempDir()
	python := filepath.Join(root, "python")
	// This sentinel is never executed: factory construction must perform no probe.
	if err := os.WriteFile(python, []byte("must never execute"), 0700); err != nil {
		t.Fatal(err)
	}
	pin := sha256.Sum256([]byte("qualified runtime"))
	digest := hex.EncodeToString(pin[:])
	baseline := interchangeReviewStyleConfig{Schema: "interchange-review-style-config/v1", Python: python, WorkerRoot: root, ScratchRoot: root, ScratchBytes: 1 << 20, TimeoutSeconds: 30, RuntimeSHA: digest, Models: map[string]string{}}
	for _, change := range []string{"valid", "sha", "unknown", "trailing", "timeout", "runtime", "models", "scratch", "python", "worker", "schema", "icc"} {
		t.Run(change, func(t *testing.T) {
			cfg := baseline
			switch change {
			case "timeout":
				cfg.TimeoutSeconds = 601
			case "runtime":
				cfg.RuntimeSHA = "invalid"
			case "models":
				cfg.Models = nil
			case "scratch":
				cfg.ScratchBytes = 1
			case "python":
				cfg.Python = root
			case "worker":
				cfg.WorkerRoot = python
			case "schema":
				cfg.Schema = "unknown"
			}
			raw, err := canonical.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if change == "unknown" {
				raw = append(raw[:len(raw)-1], []byte(`,"unexpected":true}`)...)
			}
			if change == "trailing" {
				raw = append(raw, []byte(` {}`)...)
			}
			hash := sha256.Sum256(raw)
			configPath := filepath.Join(root, "config.json")
			if err := os.WriteFile(configPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PHOTO_V30_IMPORT_STYLE_CONFIG", configPath)
			configuredPin := hex.EncodeToString(hash[:])
			if change == "sha" {
				configuredPin = digest
			}
			t.Setenv("PHOTO_V30_IMPORT_STYLE_CONFIG_SHA256", configuredPin)
			icc := digest
			if change == "icc" {
				icc = "invalid"
			}
			factory, err := buildInterchangeReviewStyle(icc)
			if change == "valid" {
				if err != nil || factory == nil {
					t.Fatal("valid pinned configuration rejected", err)
				}
			} else {
				if err == nil || factory != nil {
					t.Fatal("invalid pinned configuration accepted", change)
				}
			}
		})
	}
}

func TestInterchangeReviewStyleRejectsLinkedOperatorConfig(t *testing.T) {
	root := t.TempDir()
	python := filepath.Join(root, "python")
	if err := os.WriteFile(python, []byte("not executed"), 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("qualified runtime"))
	pin := hex.EncodeToString(digest[:])
	raw, err := canonical.Marshal(interchangeReviewStyleConfig{Schema: "interchange-review-style-config/v1", Python: python, WorkerRoot: root, ScratchRoot: root, ScratchBytes: 1 << 20, TimeoutSeconds: 30, RuntimeSHA: pin, Models: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "config.json")
	if err := os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	configSHA := sha256.Sum256(raw)
	t.Setenv("PHOTO_V30_IMPORT_STYLE_CONFIG_SHA256", hex.EncodeToString(configSHA[:]))
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(root, kind+".json")
			if kind == "symlink" {
				err = os.Symlink(config, path)
			} else {
				err = os.Link(config, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("PHOTO_V30_IMPORT_STYLE_CONFIG", path)
			if factory, err := buildInterchangeReviewStyle(pin); err == nil || factory != nil {
				t.Fatal("linked operator config accepted")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}
