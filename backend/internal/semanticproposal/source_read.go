package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// SQLTransferSourceReader uses the existing editor-scoped draft artifact catalog.
// ICC and child NPY bytes must subsequently be qualified by the input resolver.
type SQLTransferSourceReader struct {
	DB                      *pgxpool.Pool
	Objects                 v2input.SessionAnalysisObjectReader
	Bucket                  string
	ProjectID               uuid.UUID
	ManualRasterLimits      ManualRasterVerificationLimits
	ManualRasterBatchLimits ManualRasterVerificationLimits
	manualRasterBudget      *manualRasterBudget
}

func (s SQLTransferSourceReader) Read(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: binding.AssetVersionID}
	node := v2draft.LayerRef{Ref: binding.Source, Contract: "WorkingImage", CanvasHash: binding.CanvasHash}
	object, err := catalog.Lookup(ctx, who, tenantID, node)
	if err != nil {
		return nil, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, binding.Source)
	if err != nil {
		return nil, err
	}
	if _, err := v2input.ResolveSemanticSourceMetadata(binding, body); err != nil {
		return nil, err
	}
	// Object IO does not retain database locks. Repeat live scope/pins afterwards.
	current, err := catalog.Lookup(ctx, who, tenantID, node)
	if err != nil {
		return nil, err
	}
	if current != object {
		return nil, v2input.ErrConflict
	}
	return json.RawMessage(body), nil
}
