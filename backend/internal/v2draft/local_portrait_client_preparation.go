package v2draft

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// LocalPortraitClientPreparationService checks live editor scope and original
// manual producer lineage. It creates no draft or execution entitlement.
type LocalPortraitClientPreparationService struct {
	Scheduler *pgxpool.Pool
	Tenant    uuid.UUID
	Project   uuid.UUID
}

func (s LocalPortraitClientPreparationService) PrepareProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	request LocalPortraitClientSelection) (LocalPortraitClientSelection, error) {
	var zero LocalPortraitClientSelection
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Scheduler == nil || tenant == uuid.Nil || project == uuid.Nil || tenant != s.Tenant || project != s.Project || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return zero, err
	}
	request, err = DecodeLocalPortraitClientSelection(raw)
	if err != nil {
		return zero, err
	}
	run, err := v2input.RunID(tenant, who.UserID, key)
	if err != nil {
		return zero, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	if _, err = v2input.TaskID(workflow, request.Operation, request.Ordinal); err != nil {
		return zero, err
	}
	var selected struct {
		Base      v2input.SessionVersionBinding `json:"base"`
		Mask      v2input.Ref                   `json:"mask"`
		Protected v2input.Ref                   `json:"protected"`
	}
	if json.Unmarshal(request.Params, &selected) != nil {
		return zero, v2input.ErrInvalid
	}
	if _, err = v2portrait.ReadManualPairOrigin(ctx, s.Scheduler, who, tenant, project, request.Snapshot, selected.Base, selected.Mask, selected.Protected, request.TargetPixel, request.ProtectedPixel); err != nil {
		return zero, err
	}
	return request, nil
}
