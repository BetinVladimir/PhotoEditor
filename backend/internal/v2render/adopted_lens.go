package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type AdoptedLensBinding struct {
	ResultID uuid.UUID
	Result   v2input.Ref
	Base     v2input.SessionVersionBinding
}

type SQLAdoptedLensSource struct{ DB *pgxpool.Pool }

// Load runs with photo_api under a tenant transaction. It proves the exact
// expected adopted root before callers resolve JSON children. This is not a
// general artifact catalog permission or a replacement for byte verification.
func (s SQLAdoptedLensSource) Load(ctx context.Context, who auth.Identity, tenantID, project, version, producer uuid.UUID, expected v2input.Ref) (AdoptedLensBinding, error) {
	empty := AdoptedLensBinding{}
	if s.DB == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenantID == uuid.Nil || project == uuid.Nil || version == uuid.Nil || producer == uuid.Nil || !validLayerArtifactRef(expected) {
		return empty, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var binding AdoptedLensBinding
	binding.Base.AssetVersionID = version
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		err := tx.QueryRow(ctx, `SELECT result_id,artifact_id,sha256,source_artifact_id,source_sha256,canvas_hash
   FROM photo.v25_adopted_lens_binding($1,$2,$3,$4,$5,$6)`, tenantID, project, version, producer, who.UserID, who.SessionID).Scan(&binding.ResultID, &binding.Result.ArtifactID, &binding.Result.SHA256, &binding.Base.Source.ArtifactID, &binding.Base.Source.SHA256, &binding.Base.CanvasHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return v2input.ErrExpired
		}
		if err != nil {
			return err
		}
		return validateAdoptedLensBinding(binding, expected, version)
	})
	if err != nil {
		return empty, err
	}
	return binding, nil
}

func validateAdoptedLensBinding(binding AdoptedLensBinding, expected v2input.Ref, version uuid.UUID) error {
	if binding.ResultID == uuid.Nil || version == uuid.Nil || binding.Base.AssetVersionID != version || binding.Result != expected || !validLayerArtifactRef(binding.Result) || !validLayerArtifactRef(binding.Base.Source) || !validLayerArtifactRef(v2input.Ref{ArtifactID: version, SHA256: binding.Base.CanvasHash}) {
		return v2input.ErrInvalid
	}
	return nil
}
