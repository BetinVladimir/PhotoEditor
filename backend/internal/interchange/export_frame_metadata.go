package interchange

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type ExportFrameMetadata struct {
	ExternalID string
	Rating     int
	Pick       string
	Label      *string
}

func exportPickFromCollectionFlag(flag string) (string, error) {
	switch flag {
	case "pick":
		return "picked", nil
	case "reject":
		return "rejected", nil
	case "none":
		return "unmarked", nil
	}
	return "", v2input.ErrInvalid
}

// Metadata is read only from the selected collection item at the snapshot's
// revision. The export plan then freezes these values; absent provenance is not
// silently converted to zero rating/unmarked. Non-collection snapshots require
// their own explicit metadata provenance path.
func ReadExportFrameMetadata(ctx context.Context, tx ProofTransaction, who auth.Identity, tid, project, snapshot uuid.UUID, item v2input.SessionSnapshotItem) (ExportFrameMetadata, error) {
	var zero ExportFrameMetadata
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil || tid == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || item.Ordinal < 0 || item.Ordinal > 4999 || item.Binding.AssetVersionID == uuid.Nil || !validImportRef(item.Binding.Source) {
		return zero, v2input.ErrInvalid
	}
	var rating int
	var flag string
	var label *string
	err := tx.QueryRow(ctx, `SELECT i.rating,i.flag,i.color_label FROM photo.snapshots s JOIN photo.snapshot_items si ON si.tenant_id=s.tenant_id AND si.snapshot_id=s.id JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id JOIN photo.collection_items i ON i.tenant_id=si.tenant_id AND i.id=si.item_id AND i.collection_id=c.id WHERE s.tenant_id=$1 AND s.id=$2 AND c.project_id=$3 AND c.revision=s.source_revision AND c.state<>'archived' AND si.ordinal=$4 AND si.asset_version_id=$5 AND si.artifact_id=$6 AND si.sha256=$7 AND i.asset_version_id=si.asset_version_id AND photo.v25_lens_snapshot_ready($1,$3,$2,$8,$9) AND EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants t ON t.id=p.tenant_id WHERE p.tenant_id=$1 AND p.id=$3 AND p.archived_at IS NULL AND p.download_allowed AND t.status='active') FOR SHARE OF s,si,c,i`, tid, snapshot, project, item.Ordinal, item.Binding.AssetVersionID, item.Binding.Source.ArtifactID, item.Binding.Source.SHA256, who.UserID, who.SessionID).Scan(&rating, &flag, &label)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	pick, err := exportPickFromCollectionFlag(flag)
	if err != nil || rating < 0 || rating > 5 || !validImportColorLabel(label) {
		return zero, v2input.ErrInvalid
	}
	return ExportFrameMetadata{ExternalID: item.Binding.AssetVersionID.String(), Rating: rating, Pick: pick, Label: label}, nil
}

type SQLExportFrameMetadataSource struct {
	DB                        *pgxpool.Pool
	Tenant, Project, Snapshot uuid.UUID
	Who                       auth.Identity
}

func (s SQLExportFrameMetadataSource) Read(ctx context.Context, item v2input.SessionSnapshotItem) (ExportFrameMetadata, error) {
	if s.DB == nil {
		return ExportFrameMetadata{}, domain.NewError("INTERCHANGE_UNAVAILABLE", 503)
	}
	var result ExportFrameMetadata
	err := tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var err error
		result, err = ReadExportFrameMetadata(ctx, tx, s.Who, s.Tenant, s.Project, s.Snapshot, item)
		return err
	})
	if err != nil {
		return ExportFrameMetadata{}, err
	}
	return result, nil
}
