package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/interchange"
)

type apiExportAuthority struct {
	interchange.ExportNativeAuthorityFactory
}

func TestAPIInterchangeExportDeclaration(t *testing.T) {
	for _, mode := range []string{"disabled", "authority_only", "relative", "pin_only", "uppercase", "missing_authority", "conflict", "missing_file", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path, pin := "", ""
			deps := apiProcessDependencies{}
			if mode != "disabled" {
				deps.InterchangeExportAuthority = &apiExportAuthority{}
			}
			if mode != "disabled" && mode != "authority_only" {
				path = "/missing/export.json"
				pin = strings.Repeat("a", 64)
			}
			switch mode {
			case "relative":
				path = "export.json"
			case "pin_only":
				path = ""
			case "uppercase":
				pin = strings.Repeat("A", 64)
			case "missing_authority":
				deps.InterchangeExportAuthority = nil
			case "conflict":
				deps.InterchangeExport = func(context.Context, *pgxpool.Pool, *pgxpool.Pool, *objectstore.Store, string) (interchange.ExportCreatePreparer, func(), error) {
					t.Fatal("factory invoked")
					return nil, nil, nil
				}
			case "cancelled":
				cancel()
			}
			factory, err := prepareAPIInterchangeExport(ctx, path, pin, deps)
			if mode == "disabled" {
				if err != nil || factory != nil {
					t.Fatal("disabled export changed startup")
				}
				return
			}
			if err == nil || factory != nil {
				t.Fatal("invalid declaration accepted")
			}
			if mode == "cancelled" && err != context.Canceled {
				t.Fatal("lost cancellation")
			}
		})
	}
}

func TestAPIInterchangeExportPinnedNativeFactory(t *testing.T) {
	hash := func(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
	pin := strings.Repeat("a", 64)
	report, err := canonical.Marshal(interchange.ExportResourceMeasurements{Schema: "interchange-export-resource-measurements/v1", Runtime: pin, Contracts: pin, Hardware: pin, Measurements: []interchange.ExportResourceMeasurement{{FrozenSHA: pin, InputDigest: pin, RAM: 2 << 30, Scratch: 2 << 30, Output: 1 << 30, InputJSON: 128 << 20}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(map[string]any{"schema_version": "interchange-export-deployment/v1", "python": "/qualified/python", "worker_root": "/qualified/workers", "scratch_root": "/qualified/scratch", "scratch_bytes": int64(8 << 30), "timeout_seconds": 60, "image_runtime_sha256": pin, "array_runtime_sha256": pin, "runtime_sha256": pin, "contracts_sha256": pin, "hardware_sha256": pin, "measurement_sha256": hash(report), "measurements": json.RawMessage(report), "max_input_bytes": int64(8 << 30), "archive_bytes": int64(8 << 30), "expanded_bytes": int64(16 << 30), "files": 30000})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "export.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	factory, err := prepareAPIInterchangeExport(context.Background(), path, hash(raw), apiProcessDependencies{InterchangeExportAuthority: &apiExportAuthority{}})
	if err != nil || factory == nil {
		t.Fatal("pinned native factory unavailable", err)
	}
	// Preparation owns the decoded document; later file mutation does not change it.
	if err = os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	preparer, _, err := factory(ctx, nil, nil, nil, "photos")
	if err != context.Canceled || preparer != nil {
		t.Fatal("cancelled native factory performed installation", err)
	}
	if _, err = prepareAPIInterchangeExport(context.Background(), path, hash(raw), apiProcessDependencies{InterchangeExportAuthority: &apiExportAuthority{}}); err == nil {
		t.Fatal("changed deployment accepted")
	}
}
