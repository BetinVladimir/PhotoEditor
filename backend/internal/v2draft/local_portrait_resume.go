package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type LocalPortraitOwnerLoader interface {
	Load(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (LocalPortraitSavedOwner, error)
}

// Resume loads the live persisted owner and selects prepublication or staged
// recovery. Request/policy/reservation values come from the owner store, never
// from a retrying client. Material still requires current scoped verification.
func (c LocalPortraitCoordinator) Resume(ctx context.Context, loader LocalPortraitOwnerLoader,
	who auth.Identity, tenant, run uuid.UUID, sourceRaw []byte, proof LocalPortraitRecoveryProof,
	runtimeSHA string, capacity v2portrait.LocalResourceEstimate) (LocalPortraitAdmission, error) {
	var zero LocalPortraitAdmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if loader == nil || tenant == uuid.Nil || run == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	// Snapshot material before invoking the database/external loader.
	sourceRaw = append([]byte(nil), sourceRaw...)
	proof.RootRaw = append([]byte(nil), proof.RootRaw...)
	proof.OriginalRaw = append([]byte(nil), proof.OriginalRaw...)
	proof.VectorsRaw = append([]byte(nil), proof.VectorsRaw...)
	saved, err := loader.Load(ctx, who, tenant, run)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if len(saved.Request) < 2 || len(saved.Request) > 1048576 || len(saved.TaskPlan) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	// Defensive binding even for alternate loader implementations.
	var request LocalPortraitOwnerRequest
	if json.Unmarshal(saved.Request, &request) != nil || request.Run != run || request.Tenant != tenant || request.Actor != who.UserID || request.Session != who.SessionID {
		return zero, v2input.ErrInvalid
	}
	if len(saved.TaskPlan) == 0 {
		return c.Submit(ctx, who, tenant, saved.Request, sourceRaw, proof, runtimeSHA, capacity, saved.PolicySHA, saved.ReservedBytes)
	}
	return c.Recover(ctx, who, tenant, saved.Request, saved.TaskPlan, sourceRaw, proof, runtimeSHA, capacity, saved.PolicySHA, saved.ReservedBytes)
}

var _ LocalPortraitOwnerLoader = SQLLocalPortraitOwnerScope{}
