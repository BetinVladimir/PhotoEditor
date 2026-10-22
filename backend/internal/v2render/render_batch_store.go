package v2render

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/billing"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

// BeginBatch atomically persists the parent and every full child draft, quota
// reservation and blocked task. No object publication or activation occurs.
func (s SQLRenderDraftStore) BeginBatch(ctx context.Context, tenant, batch, project, snapshot uuid.UUID, who auth.Identity, policySHA string, parts []CompiledRenderBatchPart) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil {
		return v2input.ErrInvalid
	}
	if s.SubmissionSHA != "" {
		if _, err := domain.ParseDigest(s.SubmissionSHA); err != nil {
			return v2input.ErrInvalid
		}
	}
	if s.SubmissionKey != "" {
		expected, err := v2input.RunID(tenant, who.UserID, s.SubmissionKey)
		if err != nil || expected != batch || s.SubmissionSHA == "" {
			return v2input.ErrInvalid
		}
	}
	var qualificationPin []byte
	if s.QualificationPin != nil {
		pin := *s.QualificationPin
		if err := pin.validate(); err != nil {
			return err
		}
		var err error
		qualificationPin, err = json.Marshal(pin)
		if err != nil {
			return err
		}
	}
	raw, err := BuildRenderBatchManifest(batch, project, snapshot, who, policySHA, parts)
	if err != nil {
		return err
	}
	manifestSHA, err := canonical.Hash(raw)
	if err != nil {
		return err
	}
	// Own config bytes before a retryable DB callback receives control.
	configs := make([]string, len(parts))
	for i, part := range parts {
		configs[i] = string(part.Plan.Config)
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		billable, billingErr := billing.PrepareRender(ctx, tx, tenant, project, who)
		if billingErr != nil {
			return billingErr
		}
		if s.SubmissionKey != "" {
			var outcome string
			if err := tx.QueryRow(ctx, "SELECT photo.claim_v26_render_submission($1,$2,$3,$4,$5,$6,$7)", tenant, project, who.UserID, who.SessionID, batch, s.SubmissionKey, s.SubmissionSHA).Scan(&outcome); err != nil {
				return err
			}
			switch outcome {
			case "created", "pending", "replayed":
			case "conflict":
				return v2input.ErrConflict
			case "expired":
				return v2input.ErrExpired
			default:
				return v2input.ErrInvalid
			}
		}
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT photo.begin_v26_render_batch($1,$2,$3,$4,$5,$6,$7)`, tenant, project, snapshot, who.UserID, who.SessionID, batch, string(raw)).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			// Only an independently authorized existing manifest can establish
			// a changed replay. Missing/denied/cancelled evidence stays expired.
			var savedSHA string
			var cancelled bool
			err := tx.QueryRow(ctx, `SELECT manifest_sha,cancel_requested FROM photo.v26_render_batch_manifest($1,$2,$3,$4)`, tenant, batch, who.UserID, who.SessionID).Scan(&savedSHA, &cancelled)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if err == nil && !cancelled && savedSHA != manifestSHA {
				return v2input.ErrConflict
			}
			return v2input.ErrExpired
		}
		if s.SubmissionSHA != "" {
			var outcome string
			if err := tx.QueryRow(ctx, "SELECT photo.bind_v26_render_submission($1,$2,$3,$4,$5)", tenant, batch, who.UserID, who.SessionID, s.SubmissionSHA).Scan(&outcome); err != nil {
				return err
			}
			switch outcome {
			case "bound":
			case "conflict":
				return v2input.ErrConflict
			case "expired":
				return v2input.ErrExpired
			default:
				return v2input.ErrInvalid
			}
		}
		for i, config := range configs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT photo.begin_v26_render_batch_part($1,$2,$3,$4,$5,$6)`, tenant, batch, who.UserID, who.SessionID, i, config).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return v2input.ErrExpired
			}
		}
		if len(qualificationPin) != 0 {
			var outcome string
			if err := tx.QueryRow(ctx, "SELECT photo.bind_v26_render_qualification($1,$2,$3,$4,$5::jsonb)", tenant, batch, who.UserID, who.SessionID, string(qualificationPin)).Scan(&outcome); err != nil {
				return err
			}
			switch outcome {
			case "bound":
			case "conflict":
				return v2input.ErrConflict
			case "expired":
				return v2input.ErrExpired
			case "unavailable":
				return renderUnavailable()
			default:
				return v2input.ErrInvalid
			}
		}
		if billable {
			if err := billing.BindRender(ctx, tx, tenant, batch, who); err != nil {
				return err
			}
		}
		return nil
	})
}

// LoadBatch returns scoped immutable identity for reconciliation. Child plans
// not yet staged cannot be reconstructed from hashes alone.
func (s SQLRenderDraftStore) LoadBatch(ctx context.Context, tenant, batch uuid.UUID, who auth.Identity) (RecoveredRenderBatch, error) {
	if err := ctx.Err(); err != nil {
		return RecoveredRenderBatch{}, err
	}
	if s.DB == nil || tenant == uuid.Nil || batch == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return RecoveredRenderBatch{}, v2input.ErrInvalid
	}
	saved := RecoveredRenderBatch{BatchID: batch}
	var raw []byte
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, `SELECT manifest,manifest_sha,cancel_requested FROM photo.v26_render_batch_manifest($1,$2,$3,$4)`, tenant, batch, who.UserID, who.SessionID).Scan(&raw, &saved.ManifestSHA, &saved.CancelRequested)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RecoveredRenderBatch{}, v2input.ErrExpired
	}
	if err != nil {
		return RecoveredRenderBatch{}, err
	}
	saved.Manifest, err = DecodeRenderBatchManifest(batch, who.SessionID, raw, saved.ManifestSHA)
	if err != nil {
		return RecoveredRenderBatch{}, err
	}
	return saved, nil
}

type RecoveredRenderBatch struct {
	BatchID         uuid.UUID
	Manifest        RenderBatchManifest
	ManifestSHA     string
	CancelRequested bool
}

// LoadBatchPart recovers full draft evidence through a single parent-scoped
// query. Cancelled batches, missing/unsealed-ineligible parts and stale quota
// cannot resume input publication through this method.
func (s SQLRenderDraftStore) LoadBatchPart(ctx context.Context, tenant, batch uuid.UUID, who auth.Identity, ordinal int) (RecoveredRenderDraft, error) {
	if err := ctx.Err(); err != nil {
		return RecoveredRenderDraft{}, err
	}
	if s.DB == nil || tenant == uuid.Nil || batch == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ordinal < 0 || ordinal >= 5000 {
		return RecoveredRenderDraft{}, v2input.ErrInvalid
	}
	var result RecoveredRenderDraft
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, `SELECT run_id,workflow_id,project_id,snapshot_id,config,config_sha,policy_sha,reserved_bytes FROM photo.v26_render_batch_part_state($1,$2,$3,$4,$5)`, tenant, batch, who.UserID, who.SessionID, ordinal).Scan(&result.RunID, &result.WorkflowID, &result.ProjectID, &result.SnapshotID, &result.Config, &result.ConfigSHA, &result.PolicySHA, &result.ReservedBytes)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RecoveredRenderDraft{}, v2input.ErrExpired
	}
	if err != nil {
		return RecoveredRenderDraft{}, err
	}
	expected := uuid.NewSHA1(batch, []byte("render.batch:"+strconv.Itoa(ordinal)))
	if result.RunID != expected {
		return RecoveredRenderDraft{}, v2input.ErrInvalid
	}
	result.Config, err = canonical.Bytes(result.Config)
	if err != nil {
		return RecoveredRenderDraft{}, err
	}
	if _, err := DecodeRecoveredRenderPlan(result); err != nil {
		return RecoveredRenderDraft{}, err
	}
	return result, nil
}

// CancelBatch requests cancellation on the parent and matching child runs.
// Existing task drain/heartbeat/reaper paths settle attempts and resources.
func (s SQLRenderDraftStore) CancelBatch(ctx context.Context, tenant, batch uuid.UUID, who auth.Identity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || batch == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT photo.cancel_v26_render_batch($1,$2,$3,$4)`, tenant, batch, who.UserID, who.SessionID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return v2input.ErrExpired
		}
		return nil
	})
}
