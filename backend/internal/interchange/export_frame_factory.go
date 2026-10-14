package interchange

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type SQLExportFrameEvidenceSource struct {
	Recipes  SQLExportRecipeEvidenceSource
	Metadata SQLExportFrameMetadataSource
	Encoded  SQLExportOriginalSource
	Format   string
}

func (s SQLExportFrameEvidenceSource) ReadExportFrameEvidence(ctx context.Context, item v2input.SessionSnapshotItem) (ExportFrameEvidence, error) {
	var zero ExportFrameEvidence
	portable, frozenRaw, portableRaw, err := s.Recipes.Resolve(ctx, item)
	if err != nil {
		return zero, err
	}
	encoded, err := s.Encoded.Read(ctx, item)
	if err != nil {
		return zero, err
	}
	if s.Format == "captureone_sidecar" && encoded.MediaType != "image/tiff" {
		return zero, domain.NewError("INTERCHANGE_FORMAT_UNAVAILABLE", 503)
	}
	metadata, err := s.Metadata.Read(ctx, item)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ExportFrameEvidence{Frame: ExportFrame{ExternalID: metadata.ExternalID, Path: encoded.Path, Content: encoded.Ref, PortableRecipe: portable, Rating: metadata.Rating, Pick: metadata.Pick, Label: metadata.Label}, FrozenRecipe: frozenRaw, PortableRecipe: portableRaw}, nil
}

type SQLExportProposalFrameFactory struct {
	DB       *pgxpool.Pool
	Objects  ImportDocumentObjects
	Bucket   string
	Native   ExportBinaryQualifier
	MaxBytes int64
}

func (s SQLExportProposalFrameFactory) ExportProposalFrames(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, ref v2input.Ref, raw []byte) (ExportFrameResolver, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.Objects == nil || s.Native == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || s.MaxBytes < 1 || s.MaxBytes > 64<<30 {
		return nil, v2input.ErrInvalid
	}
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return nil, err
	}
	snapshot, err := v2input.DecodePinnedSessionInputSnapshot(ref, raw)
	if err != nil {
		return nil, err
	}
	if intent.Request.Direction != "export" || snapshot.SnapshotID != intent.Identity.Snapshot {
		return nil, v2input.ErrInvalid
	}
	source := SQLExportFrameEvidenceSource{
		Format:   intent.Request.Format,
		Recipes:  SQLExportRecipeEvidenceSource{DB: s.DB, Tenant: tid, Project: request.ProjectID, Snapshot: snapshot.SnapshotID, Who: who, Bucket: s.Bucket, Objects: s.Objects},
		Metadata: SQLExportFrameMetadataSource{DB: s.DB, Tenant: tid, Project: request.ProjectID, Snapshot: snapshot.SnapshotID, Who: who},
		Encoded:  SQLExportOriginalSource{DB: s.DB, Tenant: tid, Project: request.ProjectID, Snapshot: snapshot.SnapshotID, Who: who, Bucket: s.Bucket, Objects: s.Objects, Native: s.Native, MaxBytes: s.MaxBytes},
	}
	resolver, err := NewExportLineageFrameResolver(ref, append([]byte(nil), raw...), source)
	if err != nil {
		return nil, err
	}
	return resolver, nil
}

var _ ExportProposalFrameFactory = SQLExportProposalFrameFactory{}
var _ ExportFrameEvidenceSource = SQLExportFrameEvidenceSource{}
