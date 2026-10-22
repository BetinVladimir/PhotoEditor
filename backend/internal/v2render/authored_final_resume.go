package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

var ErrAuthoredFinalPending = errors.New("authored final pending")

// ResumeAdmission never re-stages a sealed/adopted child. An adopted return is
// only a locator for independent physical readback; it cannot append a phase,
// complete a producer, clear review_required or settle the parent.
func (s SQLAuthoredFinalPreparation) ResumeAdmission(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int, objects artifactjson.Objects) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Params.Continuation.Scheduler == nil || s.API == nil || objects == nil || tenant == uuid.Nil || parent == uuid.Nil || ordinal < 1 || ordinal > 4999 {
		return "", v2input.ErrInvalid
	}
	source, present, err := ReadAuthoredInstructionSource(ctx, s.Params.Continuation.Scheduler, tenant, parent, ordinal)
	if err != nil {
		return "", err
	}
	if !present {
		return "", v2input.ErrExpired
	}
	if source.Actor != who.UserID || source.Session != who.SessionID {
		return "", v2input.ErrInvalid
	}
	var state *string
	err = database.TenantTransaction(ctx, s.Params.Continuation.Scheduler, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.authored_render_final_preparation_state($1,$2,$3)`, tenant, parent, ordinal).Scan(&state)
	})
	if err != nil {
		return "", err
	}
	if state == nil {
		return "", v2input.ErrExpired
	}
	switch *state {
	case "pending":
		return "pending", ErrAuthoredFinalPending
	case "adopted":
		return "adopted", ctx.Err()
	case "absent", "draft":
		plan, err := s.Prepare(ctx, who, tenant, parent, ordinal)
		if err != nil {
			return "", err
		}
		inventory, present, err := ReadAuthoredFinalInventory(ctx, s.Params.Continuation.Scheduler, tenant, parent, ordinal)
		if err != nil {
			return "", err
		}
		if !present {
			return "", v2input.ErrExpired
		}
		required, err := authoredFinalRequiresNativeReplay(inventory, plan)
		if err != nil {
			return "", err
		}
		admission := SQLAuthoredFinalAdmission{Scheduler: s.Params.Continuation.Scheduler}
		if required {
			if s.NativeReplays == nil {
				return "", renderUnavailable()
			}
			if err = admission.Stage(ctx, tenant, parent, ordinal, plan); err != nil {
				return "", err
			}
			actualRequired, ready, err := s.NativeReplays.PrepareRequiredFinalReplays(ctx, who, tenant, parent, ordinal, plan.Task.ID)
			if err != nil {
				return "", err
			}
			if !actualRequired {
				return "", v2input.ErrExpired
			}
			if !ready {
				return "pending", ErrAuthoredFinalPending
			}
			entries, ready, err := s.NativeReplays.Native.ReadyConsumerEntries(ctx, who, tenant, plan.Task.ID, plan.Entries)
			if err != nil {
				return "", err
			}
			if !ready {
				return "pending", ErrAuthoredFinalPending
			}
			if err = admission.AdmitReplayTransport(ctx, tenant, parent, ordinal, plan, entries, s.Params.Continuation.Bucket, objects); err != nil {
				return "", err
			}
			return "pending", ErrAuthoredFinalPending
		}
		if err = admission.Admit(ctx, tenant, parent, ordinal, plan, s.Params.Continuation.Bucket, objects); err != nil {
			return "", err
		}
		return "pending", ErrAuthoredFinalPending
	default:
		return "", v2input.ErrInvalid
	}
}
