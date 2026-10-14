package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type nativeRecoveredTransport struct {
	SchemaVersion int           `json:"schema_version"`
	TaskID        uuid.UUID     `json:"task_id"`
	Manifest      v2input.Ref   `json:"manifest"`
	Chunks        []v2input.Ref `json:"chunks"`
	Count         int           `json:"count"`
	EntriesSHA    string        `json:"entries_sha"`
}

// NULL is a conflict, never permission to recreate or repair a saved graph.
// Recovery is preparatory evidence only; full sealing must revalidate authority.
func recoverNativeTransportInTransaction(ctx context.Context, tx pgx.Tx, tenant, run, task, actor, session uuid.UUID) (v2input.Built, error) {
	var zero v2input.Built
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil || tenant == uuid.Nil || run == uuid.Nil || task == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.semantic_native_registered_transport($1,$2,$3,$4,$5)", tenant, run, task, actor, session).Scan(&raw); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) == 0 {
		return zero, v2input.ErrConflict
	}
	return decodeNativeRecoveredTransport(raw, task)
}

func decodeNativeRecoveredTransport(raw []byte, task uuid.UUID) (v2input.Built, error) {
	var zero v2input.Built
	if task == uuid.Nil || len(raw) == 0 || len(raw) > 1<<20 || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return zero, v2input.ErrInvalid
	}
	var saved nativeRecoveredTransport
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&saved) != nil || decoder.Decode(new(any)) != io.EOF || saved.SchemaVersion != 1 || saved.TaskID != task || saved.Count < 3 || saved.Count > 4097 || len(saved.Chunks) != (saved.Count+v2input.ChunkSize-1)/v2input.ChunkSize {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(saved.EntriesSHA); err != nil {
		return zero, v2input.ErrInvalid
	}
	seen := make(map[uuid.UUID]bool)
	refs := append([]v2input.Ref{saved.Manifest}, saved.Chunks...)
	for _, ref := range refs {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	return v2input.Built{Manifest: saved.Manifest, Chunks: append([]v2input.Ref(nil), saved.Chunks...), Count: saved.Count, SHA256: saved.EntriesSHA}, nil
}

func recoverFrozenNativeTransport(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant, run uuid.UUID, frozen nativeRegistrationTask) (v2input.Built, error) {
	var zero v2input.Built
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if frozen == nil {
		return zero, v2input.ErrInvalid
	}
	task, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	built, err := recoverNativeTransportInTransaction(ctx, tx, tenant, run, task.ID, who.UserID, who.SessionID)
	if err != nil {
		return zero, err
	}
	if _, err := frozen.BindPublishedTransport(ctx, built); err != nil {
		return zero, err
	}
	return built, nil
}

func recoverDetectorTransportInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan detectorDraftPlan) (v2input.Built, error) {
	if err := verifyDetectorPlanSeal(ctx, plan); err != nil {
		return v2input.Built{}, err
	}
	return recoverFrozenNativeTransport(ctx, tx, who, plan.request.TenantID, plan.shell.Run, plan.prepared.frozen)
}
func recoverPersonTransportInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan personDraftPlan) (v2input.Built, error) {
	if err := verifyPersonPlanSeal(ctx, plan); err != nil {
		return v2input.Built{}, err
	}
	return recoverFrozenNativeTransport(ctx, tx, who, plan.request.TenantID, plan.shell.Run, plan.prepared.catalog.frozen)
}

// Recover and seal under the same scheduler transaction. The caller must commit
// before returning the ticket and discard it if commit has an unknown outcome.
func (s SQLTransferSourceReader) recoverAndSealDetectorInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan detectorDraftPlan) (v2input.JobTicket, error) {
	built, err := recoverDetectorTransportInTransaction(ctx, tx, who, plan)
	if err != nil {
		return v2input.JobTicket{}, err
	}
	return s.sealDetectorPreparationInTransaction(ctx, tx, who, plan, built)
}
func (s SQLTransferSourceReader) recoverAndSealPersonInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan personDraftPlan) (v2input.JobTicket, error) {
	built, err := recoverPersonTransportInTransaction(ctx, tx, who, plan)
	if err != nil {
		return v2input.JobTicket{}, err
	}
	return s.sealPersonPreparationInTransaction(ctx, tx, who, plan, built)
}
