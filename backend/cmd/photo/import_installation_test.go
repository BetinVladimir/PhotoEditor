package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/interchange"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

type schedulerImportFactoryStub struct{}
type importPersonAcceptanceStub struct{}

func (importPersonAcceptanceStub) QualifyImportedMask(context.Context, review.MaskRef, v2input.SessionVersionBinding) error {
	return nil
}

func (schedulerImportFactoryStub) BuildSchedulerInterchangeImport(context.Context, *pgxpool.Pool, *objectstore.Store, string) (workflow.InterchangeImportContinuation, error) {
	return nil, nil
}

func TestSchedulerImportFactorySelection(t *testing.T) {
	t.Setenv("PHOTO_V30_IMPORT_OPERATOR_CONFIG", "")
	t.Setenv("PHOTO_V30_IMPORT_OPERATOR_CONFIG_SHA256", "")
	got, err := schedulerImportFactories(context.Background(), apiProcessDependencies{})
	if err != nil || len(got) != 0 {
		t.Fatal("absent installation became a present nil factory")
	}
	installed, err := schedulerImportFactories(context.Background(), apiProcessDependencies{InterchangeImport: schedulerImportFactoryStub{}})
	if err != nil || len(installed) != 1 || installed[0] == nil {
		t.Fatal("trusted import factory was not passed to scheduler")
	}
}

func TestSchedulerImportOperatorInstallationFailsClosed(t *testing.T) {
	t.Setenv("PHOTO_V30_IMPORT_OPERATOR_CONFIG", "")
	t.Setenv("PHOTO_V30_IMPORT_OPERATOR_CONFIG_SHA256", "")
	files := &interchange.ImportSchedulerOperatorFiles{}
	base := &interchange.SQLImportEntryRecipeAuthorityFactory{}
	for _, deps := range []apiProcessDependencies{
		{ImportOperatorFiles: files},
		{ImportOperatorFiles: files, ImportEntryBase: base},
		{ImportOperatorFiles: files, ImportEntryBase: base, ImportPersonAcceptance: importPersonAcceptanceStub{}, InterchangeImport: schedulerImportFactoryStub{}},
	} {
		if got, err := schedulerImportFactories(context.Background(), deps); err == nil || len(got) != 0 {
			t.Fatal("partial or conflicting V30 installation reached scheduler")
		}
	}
	if got, err := schedulerImportFactories(context.Background(), apiProcessDependencies{ImportOperatorFiles: files, ImportEntryBase: base, ImportPersonAcceptance: importPersonAcceptanceStub{}}); err == nil || len(got) != 0 {
		t.Fatal("invalid operator files produced a factory")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := schedulerImportFactories(ctx, apiProcessDependencies{}); !errors.Is(err, context.Canceled) || len(got) != 0 {
		t.Fatal("scheduler import installation lost cancellation")
	}
}

func TestSchedulerImportOperatorEnvironmentRequiresPinnedTrustedPorts(t *testing.T) {
	t.Setenv("PHOTO_V30_IMPORT_OPERATOR_CONFIG", "/operator/config.json")
	t.Setenv("PHOTO_V30_IMPORT_OPERATOR_CONFIG_SHA256", "")
	if got, err := schedulerImportFactories(context.Background(), apiProcessDependencies{}); err == nil || len(got) != 0 {
		t.Fatal("unpaired operator declaration accepted")
	}
	t.Setenv("PHOTO_V30_IMPORT_OPERATOR_CONFIG_SHA256", "bad")
	if got, err := schedulerImportFactories(context.Background(), apiProcessDependencies{}); err == nil || len(got) != 0 {
		t.Fatal("operator declaration without trusted semantic ports accepted")
	}
}

func TestSchedulerImportOperatorEnvironmentLoadsPinnedDeclaration(t *testing.T) {
	sha, path := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "/operator/not-installed.json"
	file := func() map[string]string { return map[string]string{"path": path, "sha256": sha} }
	raw, err := canonical.Marshal(map[string]any{
		"schema_version": "interchange-import-scheduler-operator/v1",
		"native":         file(), "style": file(), "denoise": file(),
		"upscale": file(), "stats": file(), "person": file(),
		"max_archive_bytes": 1 << 30, "max_expanded_bytes": 2 << 30, "max_files": 30000,
	})
	if err != nil {
		t.Fatal(err)
	}
	operatorPath := filepath.Join(t.TempDir(), "operator.json")
	if err := os.WriteFile(operatorPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PHOTO_V30_IMPORT_OPERATOR_CONFIG", operatorPath)
	digest := sha256.Sum256(raw)
	t.Setenv("PHOTO_V30_IMPORT_OPERATOR_CONFIG_SHA256", hex.EncodeToString(digest[:]))
	base := &interchange.SQLImportEntryRecipeAuthorityFactory{Codecs: interchange.ImportRecipeCodecDependencies{Params: interchange.RejectUnsupportedImportDependency{}, OverrideContent: importOverrideContentStub{}}}
	deps := apiProcessDependencies{ImportEntryBase: base, ImportPersonAcceptance: importPersonAcceptanceStub{}}
	if got, err := schedulerImportFactories(context.Background(), apiProcessDependencies{}); err == nil || len(got) != 0 {
		t.Fatal("ordinary process bypassed subordinate pinned deployment validation")
	}
	if got, err := schedulerImportFactories(context.Background(), deps); err == nil || len(got) != 0 {
		t.Fatal("missing pinned subordinate deployment produced a factory")
	}
	deps.ImportOperatorFiles = &interchange.ImportSchedulerOperatorFiles{}
	if got, err := schedulerImportFactories(context.Background(), deps); err == nil || len(got) != 0 {
		t.Fatal("environment and injected declarations were combined")
	}
}

type importOverrideContentStub struct{}

func (importOverrideContentStub) QualifyImportOverrideContent(context.Context, []byte, v2input.SessionVersionBinding) error {
	return nil
}
