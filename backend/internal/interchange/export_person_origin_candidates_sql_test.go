package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestSQLPersonOriginCandidateDiscoveryFailsClosedBeforeGraphOrDatabaseIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if candidates, err := (SQLExportPersonOriginCandidates{}).PrepareExportPersonOriginCandidates(
		ctx, CreateIntent{}, FrozenExportDocuments{}, nil, SQLExportDocumentCatalog{}); err != context.Canceled || candidates != nil {
		t.Fatal("cancelled discovery touched graph or database", err)
	}
	if candidates, err := (SQLExportPersonOriginCandidates{}).PrepareExportPersonOriginCandidates(
		context.Background(), CreateIntent{}, FrozenExportDocuments{}, nil, SQLExportDocumentCatalog{}); err != v2input.ErrInvalid || candidates != nil {
		t.Fatal("unprovisioned issuer accepted", err)
	}
	if _, err := (SQLExportDocumentCatalog{}).DiscoverPortablePersonOriginRoot(context.Background(),
		v2input.SessionVersionBinding{}, v2input.Ref{}, rawSHA([]byte("root"))); err != v2input.ErrInvalid {
		t.Fatal("unbound producer root discovered", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := (SQLExportDocumentCatalog{}).DiscoverPortablePersonOriginRoot(ctx,
		v2input.SessionVersionBinding{}, v2input.Ref{ArtifactID: uuid.New()}, rawSHA([]byte("root"))); err != context.Canceled {
		t.Fatal("cancelled producer-root discovery touched database", err)
	}
}
