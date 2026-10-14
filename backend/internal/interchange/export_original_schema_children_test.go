package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestExportOriginalSchemaChildrenRefusesUnqualifiedReferences(t *testing.T) {
	validator := ExportOriginalSchemaChildren{}
	makeEntry := func(contract string, raw []byte) v2input.Entry {
		return v2input.Entry{Contract: contract, Ref: v2input.Ref{
			ArtifactID: uuid.New(), SHA256: rawSHA(raw),
		}}
	}
	style, err := canonical.Marshal(map[string]any{
		"schema_version": "2.0.0", "extensions": map[string]any{
			"background": map[string]any{"mode": "asset", "asset_id": "portable-background"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if children, err := validator.ExportDocumentChildren(context.Background(),
		makeEntry("StyleV2", style), style); err != nil || len(children) != 0 {
		t.Fatal("portable style asset rejected", err)
	}
	foreign, err := canonical.Marshal(map[string]any{"payload": map[string]any{
		"artifact_id": uuid.NewString(), "sha256": rawSHA([]byte("foreign")),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if children, err := validator.ExportDocumentChildren(context.Background(),
		makeEntry("StyleV2", foreign), foreign); err == nil || children != nil {
		t.Fatal("untyped style artifact accepted")
	}
	if children, err := validator.ExportDocumentChildren(context.Background(),
		makeEntry("Unknown", style), style); err == nil || children != nil {
		t.Fatal("unknown export contract accepted")
	}
	if children, err := validator.ExportDocumentChildren(context.Background(),
		makeEntry("StyleV2", style), append(style, ' ')); err == nil || children != nil {
		t.Fatal("modified style bytes accepted")
	}
}
