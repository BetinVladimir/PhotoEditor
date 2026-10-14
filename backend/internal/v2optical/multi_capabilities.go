package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"time"
)

type MultiCapabilities struct {
	SchemaVersion int         `json:"schema_version"`
	Tenant        uuid.UUID   `json:"tenant_id"`
	Project       uuid.UUID   `json:"project_id"`
	Operation     string      `json:"operation"`
	Calibration   v2input.Ref `json:"calibration"`
	MaxPixels     int64       `json:"max_pixels"`
	FrameLimit    int         `json:"frame_limit"`
}

func (p *MultiProjects) DescribeProject(ctx context.Context, who auth.Identity, tenant, project uuid.UUID) (MultiCapabilities, error) {
	var zero MultiCapabilities
	if p == nil {
		return zero, v2qualification.ErrUnqualified
	}
	service := p.services[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return zero, v2qualification.ErrUnqualified
	}
	return service.describe(ctx, who, tenant, project)
}
func (s *MultiService) describe(ctx context.Context, who auth.Identity, tenant, project uuid.UUID) (MultiCapabilities, error) {
	var zero MultiCapabilities
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || tenant != s.tenant || project != s.project || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	c := s.coordinator
	if c.DB == nil {
		return zero, v2input.ErrInvalid
	}
	body, err := c.Pin.Compile()
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := c.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err := (multiParamsRepository{}).authorize(ctx, tx, artifactjson.Scope{Tenant: tenant}); err != nil {
		return zero, err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.v29_multi_capabilities_ready($1,$2,$3,$4,$5::jsonb,$6)", tenant, project, who.UserID, who.SessionID, string(body), c.Operation).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2qualification.ErrUnqualified
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	limit := 20
	if c.Operation == "optical.focus_stack" {
		limit = 30
	}
	return MultiCapabilities{SchemaVersion: 1, Tenant: tenant, Project: project, Operation: c.Operation, Calibration: c.Pin.Calibration, MaxPixels: min(c.MaxPixels, c.Capacity.PixelCount), FrameLimit: limit}, nil
}
