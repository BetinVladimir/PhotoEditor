package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Submit starts a server-compiled intent using freshly authorized materials.
// Policy and reservation are compiler inputs, independently checked again by
// SQL. This composition does not expose an HTTP endpoint or invent qualification.
func (s LocalPortraitRecoveryService) Submit(ctx context.Context, who auth.Identity, tenant uuid.UUID, requestRaw []byte, policySHA string, reservedBytes int64) (LocalPortraitAdmission, error) {
	var zero LocalPortraitAdmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || (s.Materials == nil && s.Factory == nil) || len(requestRaw) < 2 || len(requestRaw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	requestRaw = append([]byte(nil), requestRaw...)
	if _, err := localPortraitOwnerConfig(requestRaw, policySHA, reservedBytes); err != nil {
		return zero, err
	}
	var request LocalPortraitOwnerRequest
	if json.Unmarshal(requestRaw, &request) != nil || request.Tenant != tenant || request.Actor != who.UserID || request.Session != who.SessionID {
		return zero, v2input.ErrInvalid
	}
	materials, coordinator := s.Materials, s.Coordinator
	if s.Factory != nil {
		var err error
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
	material, err := materials.Load(ctx, who, tenant, append([]byte(nil), requestRaw...))
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return coordinator.Submit(ctx, who, tenant, requestRaw, material.Source, material.Proof, s.RuntimeSHA, s.Capacity, policySHA, reservedBytes)
}
