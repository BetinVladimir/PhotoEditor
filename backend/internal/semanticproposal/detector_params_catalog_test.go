package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestDetectorParamsCatalogRefusesRootSubstitution(t *testing.T) {
	source := &defectSizeCatalog{}
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	catalog := detectorParamsCatalog{source: source, document: uuid.New(), params: params}
	for _, node := range []v2draft.LayerRef{
		{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: params.SHA256}, Contract: "Params"},
		{Ref: params, Contract: "Params", ParentRef: params},
		{Ref: params, Contract: "WorkingImage"},
	} {
		if _, err := catalog.Lookup(context.Background(), auth.Identity{}, uuid.New(), node); err == nil {
			t.Fatal("substituted Params root accepted")
		}
	}
	if source.calls != 0 {
		t.Fatal("invalid root reached source catalog")
	}
	node := v2draft.LayerRef{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: params.SHA256}, Contract: "WorkingPixels", ParentRef: v2input.Ref{ArtifactID: uuid.New(), SHA256: params.SHA256}}
	if _, err := catalog.Lookup(context.Background(), auth.Identity{}, uuid.New(), node); err != nil || source.calls != 1 {
		t.Fatal("source scope not delegated", err)
	}
}
