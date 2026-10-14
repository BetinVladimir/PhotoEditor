package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// RegisterWorkflowInputsInTransaction registers frozen input transport without
// activating tasks. Caller must roll back every error. All external object
// verification/publication must finish before opening this transaction.
func RegisterWorkflowInputsInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, published PublishedIntent, plan WorkflowPlan, transports []v2input.Built) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || plan.tenant != request.TenantID || plan.actor != request.ActorID || who.UserID != request.ActorID || plan.RunID() != request.RunID || plan.WorkflowID() != request.WorkflowID || plan.RequestSHA() != request.RequestSHA || plan.IntentRef() != published.Ref || !sameTransferInventory(plan.Inventory(), published.Inventory) {
		return v2input.ErrInvalid
	}
	var config v2input.SemanticDraftConfig
	if json.Unmarshal(plan.ConfigJSON(), &config) != nil || config.Semantic.AuthSessionID != who.SessionID {
		return v2input.ErrInvalid
	}
	graph, err := BindWorkflowTransport(ctx, plan, transports)
	if err != nil {
		return err
	}
	if err := lockWorkflowAdmissionEvidence(ctx, tx, who, request, published, plan); err != nil {
		return err
	}
	for i, task := range graph {
		state, err := registrationState(ctx, tx, request, plan.tasks[i], task)
		if err != nil {
			return err
		}
		if state == "replayed" {
			continue
		}
		if state != "missing" {
			return v2input.ErrConflict
		}
		if err := v2input.RegisterInTransaction(ctx, tx, request.TenantID, request.RunID, task.ID, task.Built, task.Open, task.Direct); err != nil {
			return err
		}
	}
	return nil
}

// Retains all independently prepared authority in one caller transaction.
// Registration uses this DB-only chain; completed recovery must compose it
// rather than substitute ticket existence for live authority checks.
func lockWorkflowAdmissionEvidence(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, published PublishedIntent, plan WorkflowPlan) error {
	if err := LockPreparedSourcesInTransaction(ctx, tx, who, request, plan); err != nil {
		return err
	}
	if err := LockInventoryInTransaction(ctx, tx, who, request, plan.Inventory()); err != nil {
		return err
	}
	if err := LockPublishedIntentInTransaction(ctx, tx, who, request, published); err != nil {
		return err
	}
	if err := LockSnapshotInTransaction(ctx, tx, who, request, plan); err != nil {
		return err
	}
	if err := LockQualificationInTransaction(ctx, tx, who, request, plan); err != nil {
		return err
	}
	if err := LockProtectionInTransaction(ctx, tx, who, request, plan); err != nil {
		return err
	}
	if err := LockPersonProducerInTransaction(ctx, tx, who, request, plan); err != nil {
		return err
	}
	if err := LockPreparedSourcesInTransaction(ctx, tx, who, request, plan); err != nil {
		return err
	}
	return nil
}

func registrationState(ctx context.Context, tx pgx.Tx, request Request, draft v2input.DraftTask, task v2input.GraphTask) (string, error) {
	return workflowInputState(ctx, tx, request, draft, task, nil)
}

func workflowInputState(ctx context.Context, tx pgx.Tx, request Request, draft v2input.DraftTask, task v2input.GraphTask, completed *WorkflowPlan) (string, error) {
	cursor, err := task.Open(ctx)
	if err != nil {
		return "", err
	}
	if cursor == nil {
		return "", v2input.ErrInvalid
	}
	if closer, ok := cursor.(io.Closer); ok {
		defer closer.Close()
	}
	entries := make([]v2input.Entry, 0, task.Built.Count)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		entry, ok, err := cursor.Next(ctx)
		if err != nil {
			return "", err
		}
		if !ok {
			break
		}
		if len(entries) >= 128 {
			return "", v2input.ErrInvalid
		}
		entries = append(entries, entry)
	}
	if len(entries) != task.Built.Count {
		return "", v2input.ErrInvalid
	}
	rows, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	manifest, _ := json.Marshal(task.Built.Manifest)
	chunks, _ := json.Marshal(task.Built.Chunks)
	direct, _ := json.Marshal(task.Direct)
	var state string
	query := "SELECT photo.semantic_proposal_registration_state($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)"
	args := []any{request.TenantID, request.RunID, task.ID, draft.ParamsArtifactID, draft.ParamsSHA, draft.InputDigest, draft.ExecutionProfile, json.RawMessage(rows), json.RawMessage(manifest), json.RawMessage(chunks), json.RawMessage(direct), task.Built.Count, task.Built.SHA256}
	if completed != nil {
		query = "SELECT photo.semantic_native_transfer_completed_state($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)"
		args = append(args, completed.WorkflowID(), json.RawMessage(completed.ConfigJSON()))
	}
	err = tx.QueryRow(ctx, query, args...).Scan(&state)
	return state, err
}
