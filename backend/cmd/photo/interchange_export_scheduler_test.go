package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/interchange"
)

func TestSchedulerInterchangeExportRequiresPinnedAuthority(t *testing.T) {
	if factory, err := prepareSchedulerInterchangeExport(context.Background(), "", "",
		apiProcessDependencies{}); err != nil || factory != nil {
		t.Fatalf("disabled scheduler export: %v", err)
	}
	for _, deps := range []apiProcessDependencies{
		{InterchangeExportAuthority: &apiExportAuthority{}},
		{InterchangePersonOriginTrust: map[interchange.PortablePersonOriginKey]ed25519.PublicKey{}},
	} {
		if factory, err := prepareSchedulerInterchangeExport(context.Background(), "x",
			strings.Repeat("a", 64), deps); err == nil || factory != nil {
			t.Fatal("untrusted scheduler export installed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if factory, err := prepareSchedulerInterchangeExport(ctx, "", "",
		apiProcessDependencies{}); err != context.Canceled || factory != nil {
		t.Fatalf("cancelled scheduler export: %v", err)
	}
}

func TestSchedulerInterchangeExportPinnedFactory(t *testing.T) {
	hash := func(raw []byte) string {
		digest := sha256.Sum256(raw)
		return hex.EncodeToString(digest[:])
	}
	lock := strings.Repeat("a", 64)
	report, err := canonical.Marshal(interchange.ExportResourceMeasurements{
		Schema:  "interchange-export-resource-measurements/v1",
		Runtime: lock, Contracts: lock, Hardware: lock,
		Measurements: []interchange.ExportResourceMeasurement{{
			FrozenSHA: lock, InputDigest: lock, RAM: 2 << 30,
			Scratch: 2 << 30, Output: 1 << 30, InputJSON: 128 << 20,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(map[string]any{
		"schema_version": "interchange-export-deployment/v1",
		"python":         "/qualified/python", "worker_root": "/qualified/workers",
		"scratch_root": "/qualified/scratch", "scratch_bytes": int64(8 << 30),
		"timeout_seconds": 60, "image_runtime_sha256": lock,
		"array_runtime_sha256": lock, "runtime_sha256": lock,
		"contracts_sha256": lock, "hardware_sha256": lock,
		"measurement_sha256": hash(report), "measurements": json.RawMessage(report),
		"max_input_bytes": int64(8 << 30), "archive_bytes": int64(8 << 30),
		"expanded_bytes": int64(16 << 30), "files": 30000,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key := interchange.PortablePersonOriginKey{IssuerID: uuid.New(), KeyID: hash(public)}
	deps := apiProcessDependencies{
		InterchangeExportAuthority: &apiExportAuthority{},
		InterchangePersonOrigins:   personOriginFactoryStub{},
		InterchangePersonOriginTrust: map[interchange.PortablePersonOriginKey]ed25519.PublicKey{
			key: public,
		},
	}
	factory, err := prepareSchedulerInterchangeExport(context.Background(), path, hash(raw), deps)
	if err != nil || factory == nil {
		t.Fatalf("pinned scheduler export unavailable: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if job, err := factory.BuildSchedulerInterchangeExport(ctx, nil, nil, "photos"); err != context.Canceled || job != nil {
		t.Fatalf("cancelled scheduler factory: %v", err)
	}
}
