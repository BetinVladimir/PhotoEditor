package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type LocalPortraitMaterialLoader interface {
	Load(context.Context, auth.Identity, uuid.UUID, []byte) (LocalPortraitMaterial, error)
}

// LocalPortraitRecoveryService resumes one authenticated draft. Runtime and
// resource limits are deployment settings; clients cannot override them.
// Configure scoped catalogs for the owner's exact project and asset version.
type LocalPortraitRecoveryService struct {
	Factory     LocalPortraitRecoveryFactory
	Owners      LocalPortraitOwnerLoader
	Materials   LocalPortraitMaterialLoader
	Coordinator LocalPortraitCoordinator
	RuntimeSHA  string
	Capacity    v2portrait.LocalResourceEstimate
}

func (s LocalPortraitRecoveryService) Resume(ctx context.Context, who auth.Identity, tenant, run uuid.UUID) (LocalPortraitAdmission, error) {
	var zero LocalPortraitAdmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Owners == nil || (s.Materials == nil && s.Factory == nil) || tenant == uuid.Nil || run == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	saved, err := s.Owners.Load(ctx, who, tenant, run)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if len(saved.Request) < 2 || len(saved.Request) > 1048576 || len(saved.TaskPlan) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	saved.Request = append(json.RawMessage(nil), saved.Request...)
	saved.TaskPlan = append(json.RawMessage(nil), saved.TaskPlan...)
	var request LocalPortraitOwnerRequest
	if json.Unmarshal(saved.Request, &request) != nil || request.Run != run || request.Tenant != tenant || request.Actor != who.UserID || request.Session != who.SessionID {
		return zero, v2input.ErrInvalid
	}
	if _, err = localPortraitOwnerConfig(saved.Request, saved.PolicySHA, saved.ReservedBytes); err != nil {
		return zero, err
	}
	materials, coordinator := s.Materials, s.Coordinator
	if s.Factory != nil {
		materials, coordinator, err = s.Factory.Build(ctx, request)
		if err != nil {
			return zero, err
		}
		if materials == nil {
			return zero, v2input.ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	material, err := materials.Load(ctx, who, tenant, append([]byte(nil), saved.Request...))
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if len(saved.TaskPlan) == 0 {
		return coordinator.Submit(ctx, who, tenant, saved.Request, material.Source, material.Proof, s.RuntimeSHA, s.Capacity, saved.PolicySHA, saved.ReservedBytes)
	}
	return coordinator.Recover(ctx, who, tenant, saved.Request, saved.TaskPlan, material.Source, material.Proof, s.RuntimeSHA, s.Capacity, saved.PolicySHA, saved.ReservedBytes)
}

var _ LocalPortraitMaterialLoader = SQLLocalPortraitMaterialLoader{}
