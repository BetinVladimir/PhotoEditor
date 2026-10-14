package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"time"
)

type PerspectiveApplied struct {
	Version uuid.UUID   `json:"asset_version_id"`
	Parent  uuid.UUID   `json:"parent_asset_version_id"`
	Source  v2input.Ref `json:"source"`
}

func DecodePerspectiveApplyDecision(raw []byte) (PerspectiveApplyDecision, error) {
	var value PerspectiveApplyDecision
	if len(raw) < 2 || len(raw) > 65536 {
		return value, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || json.Unmarshal(normalized, &value) != nil {
		return PerspectiveApplyDecision{}, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(value)
	if err != nil || !bytes.Equal(exact, normalized) || value.SchemaVersion != 1 || value.ResultID == uuid.Nil || value.Root.ArtifactID == uuid.Nil || value.QualityRun == uuid.Nil || value.QualityResult == uuid.Nil || value.QualityReport.ArtifactID == uuid.Nil || !validSHA256(value.Root.SHA256) || !validSHA256(value.QualityReport.SHA256) || !value.ConfirmGeometryReset {
		return PerspectiveApplyDecision{}, v2input.ErrInvalid
	}
	return value, nil
}

func (s PerspectiveProposalService) Apply(ctx context.Context, who auth.Identity, tenantID, project, version, producer uuid.UUID, decision PerspectiveApplyDecision) (PerspectiveApplied, error) {
	var zero PerspectiveApplied
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	current, err := s.QualityReview(ctx, who, tenantID, project, version, producer)
	if err != nil {
		return zero, err
	}
	if err = ValidatePerspectiveApplyDecision(decision, current); err != nil {
		return zero, err
	}
	catalog, _, err := BuildAdoptedPerspectiveCatalog(ctx, SQLAdoptedPerspectiveSource{DB: s.DB}, s.Objects, who, tenantID, project, version, producer, current.Proposal.Result, s.Bucket)
	if err != nil {
		return zero, err
	}
	image := catalog.documents[current.Proposal.WorkingImage.ArtifactID.String()]
	if len(image) < 2 {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(decision)
	if err != nil {
		return zero, err
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var saved *uuid.UUID
	err = tenant.WithTenant(bounded, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		if _, err := tx.Exec(bounded, "SET LOCAL statement_timeout='15s'"); err != nil {
			return err
		}
		if err := tx.QueryRow(bounded, "SELECT photo.apply_v29_perspective_version($1,$2,$3,$4,$5,$6,$7::jsonb,$8)", tenantID, project, version, producer, who.UserID, who.SessionID, string(body), string(image)).Scan(&saved); err != nil {
			return err
		}
		// Returning an error forces rollback, including the inner decision-save.
		expected := uuid.NewSHA1(current.QualityRun, []byte("V29.PerspectiveAppliedVersion"))
		if saved == nil || *saved != expected {
			return v2input.ErrExpired
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	return PerspectiveApplied{Version: *saved, Parent: version, Source: current.Proposal.WorkingImage}, nil
}
