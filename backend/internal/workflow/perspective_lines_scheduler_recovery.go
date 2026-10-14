package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Recovery loads saved identity/intent, then repeats source IO and the existing
// transactional activation gate. It never republishes sealed task documents.
func (s PerspectiveLinesPreparation) ResumeSealed(ctx context.Context, tenant, run uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || s.Sources == nil || s.Arrays == nil || s.Inputs == nil || tenant == uuid.Nil || run == uuid.Nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return v2input.ErrInvalid
	}
	var raw []byte
	err := withPerspectivePreviewDocumentTx(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.v29_perspective_lines_scheduler_recovery($1,$2)", tenant, run).Scan(&raw)
	})
	if err != nil {
		return fmt.Errorf("load sealed line recovery: %w", err)
	}
	plan, snapshot, who, err := parsePerspectiveLinesRecovery(raw, tenant, run, s.Policy)
	if err != nil {
		return err
	}
	verified, err := VerifyPerspectiveLinesInputs(ctx, s.Sources, s.Arrays, s.Inputs, plan, snapshot, who)
	if err != nil {
		return fmt.Errorf("verify sealed line source: %w", err)
	}
	return ActivatePerspectiveLines(ctx, s.DB, plan, verified, s.Bucket)
}

func parsePerspectiveLinesRecovery(raw []byte, tenant, run uuid.UUID, policy PerspectiveLinesExecutionPolicy) (PerspectiveLinesPlan, uuid.UUID, auth.Identity, error) {
	var zero PerspectiveLinesPlan
	invalid := func() (PerspectiveLinesPlan, uuid.UUID, auth.Identity, error) {
		return zero, uuid.Nil, auth.Identity{}, v2input.ErrInvalid
	}
	if tenant == uuid.Nil || run == uuid.Nil || len(raw) < 2 || len(raw) > 2097152 {
		return invalid()
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return invalid()
	}
	var wire struct {
		SchemaVersion int                    `json:"schema_version"`
		Intent        perspectiveLinesIntent `json:"intent"`
		Policy        string                 `json:"policy_jcs"`
	}
	if json.Unmarshal(normal, &wire) != nil {
		return invalid()
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(normal, exact) || wire.SchemaVersion != 1 || wire.Intent.SchemaVersion != 1 || wire.Intent.Tenant != tenant || wire.Intent.Run != run {
		return invalid()
	}
	i := wire.Intent
	plan, err := CompilePerspectiveLinesPlan(PerspectiveLinesScope{Tenant: tenant, Project: i.Project, Run: run, Base: i.Base}, []byte(i.SourceJCS), i.WorkingICC)
	if err != nil {
		return invalid()
	}
	who := auth.Identity{UserID: i.Actor, SessionID: i.Session}
	intent, err := CompilePerspectiveLinesIntent(plan, i.Snapshot, who)
	if err != nil {
		return invalid()
	}
	saved, err := canonical.Marshal(i)
	if err != nil || !bytes.Equal(intent, saved) {
		return invalid()
	}
	expected, err := policy.ForPlan(plan)
	if err != nil || !bytes.Equal(expected, []byte(wire.Policy)) {
		return invalid()
	}
	return plan, i.Snapshot, who, nil
}
