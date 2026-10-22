package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"time"
)

type MultiApplied struct {
	Version uuid.UUID   `json:"asset_version_id"`
	Parent  uuid.UUID   `json:"parent_asset_version_id"`
	Source  v2input.Ref `json:"source"`
}

func (s MultiProposalService) Apply(ctx context.Context, who auth.Identity, tenantID, project, producer uuid.UUID, operation string, decision MultiApplyDecision) (MultiApplied, error) {
	var zero MultiApplied
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	current, err := s.QualityReview(ctx, who, tenantID, project, producer, operation)
	if err != nil {
		return zero, err
	}
	if err = ValidateMultiApplyDecision(decision, current); err != nil {
		return zero, err
	}

	content, err := LoadAdoptedMultiContent(ctx, s.Source, s.Objects, who, tenantID, project, producer, operation, current.Proposal.Result, s.Bucket)
	if err != nil {
		return zero, err
	}
	binding, err := content.Binding()
	if err != nil || binding.ResultID != current.Proposal.ResultID {
		return zero, v2input.ErrExpired
	}
	frames, reference, _, err := binding.Inputs()
	if err != nil {
		return zero, err
	}
	root, image, witnesses, err := content.applyEvidence(tenantID, project, producer, binding)
	if err != nil {
		return zero, err
	}

	originals, err := content.originalInventory(binding, producer)
	if err != nil {
		return zero, err
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
		if err := tx.QueryRow(bounded, "SELECT photo.apply_v29_multi_complete($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10::jsonb,$11::jsonb)", tenantID, project, operation, producer, who.UserID, who.SessionID, string(body), string(root), string(image), string(witnesses), string(originals)).Scan(&saved); err != nil {
			return err
		}
		// Returning an error forces rollback, including the inner decision-save.
		expected := uuid.NewSHA1(current.QualityRun, []byte("V29.MultiAppliedVersion"))
		if saved == nil || *saved != expected {
			return v2input.ErrExpired
		}
		return bounded.Err()
	})
	if cancellation := bounded.Err(); cancellation != nil {
		return zero, cancellation
	}
	if err != nil {
		return zero, err
	}
	return MultiApplied{Version: *saved, Parent: frames[reference].AssetVersionID, Source: current.Proposal.WorkingImage}, nil
}
