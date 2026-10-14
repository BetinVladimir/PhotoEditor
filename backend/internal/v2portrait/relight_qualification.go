package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// LockRelightQualification retains current policy/report/calibration locks in
// the caller transaction. Verify original evidence separately before invoking;
// this gate neither imports qualification nor admits or activates a task.
func LockRelightQualification(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant, project uuid.UUID, pin RelightQualificationPin) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	body, err := pin.Compile()
	if err != nil {
		return err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT photo.lock_v28_relight_qualification($1,$2,$3,$4,$5::jsonb)", tenant, project, who.UserID, who.SessionID, string(body)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}

// RelightQualificationPin is a distinct operation-specific structural pin.
// Compiling it never imports a report or establishes catalog/issuer trust.
type RelightQualificationPin DonorQualificationPin

func (p RelightQualificationPin) Compile() ([]byte, error) {
	return DonorQualificationPin(p).Compile()
}

// CompilePinnedRelightTaskPlan binds the selected catalog pin to Params and
// runtime. Live catalog, policy/report bytes and producer verification remain
// mandatory at publication/admission; this function grants no authority.
func CompilePinnedRelightTaskPlan(ctx context.Context, binding v2input.SessionVersionBinding,
	sourceRaw, paramsRaw []byte, runtimeSHA string, capacity LocalResourceEstimate,
	pin RelightQualificationPin) (LocalTaskPlan, error) {
	var zero LocalTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if _, err := pin.Compile(); err != nil {
		return zero, err
	}
	plan, err := CompileRelightTaskPlan(ctx, binding, sourceRaw, paramsRaw, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	var params RelightParams
	if json.Unmarshal(plan.Params, &params) != nil || params.Calibration != pin.Calibration || runtimeSHA != pin.RuntimeLockSHA {
		return zero, v2input.ErrInvalid
	}
	if _, err = RelightSourceInputs(params, pin, sourceRaw); err != nil {
		return zero, err
	}
	var profile map[string]json.RawMessage
	if json.Unmarshal(plan.Profile, &profile) != nil {
		return zero, v2input.ErrInvalid
	}
	profile["relight_qualification"], err = pin.Compile()
	if err != nil {
		return zero, err
	}
	plan.Profile, err = canonical.Marshal(profile)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return plan, nil
}
