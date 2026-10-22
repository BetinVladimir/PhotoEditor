package semanticreview

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/idempotency"
)

const CloseIdempotencyRoute = "SemanticIntent.CloseReview"

type CloseResult struct {
	RunID      uuid.UUID         `json:"run_id"`
	WorkflowID uuid.UUID         `json:"workflow_id"`
	Revision   string            `json:"revision"`
	State      string            `json:"state"`
	Outputs    []json.RawMessage `json:"outputs"`
	ErrorCode  *string           `json:"error_code"`
}

// DecodeCloseResult accepts only the terminal decision-closure response.
// Outputs and child completion belong to their own jobs.
func DecodeCloseResult(raw []byte) (CloseResult, error) {
	var result CloseResult
	var fields map[string]json.RawMessage
	if decodeStrict(raw, &result) != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 6 ||
		result.RunID == uuid.Nil || result.WorkflowID == uuid.Nil || !validRevision(result.Revision) || result.Revision == "1" ||
		result.State != "completed" || result.Outputs == nil || len(result.Outputs) != 0 || result.ErrorCode != nil {
		return CloseResult{}, ErrInvalid
	}
	for _, name := range []string{"run_id", "workflow_id", "revision", "state", "outputs", "error_code"} {
		if _, exists := fields[name]; !exists {
			return CloseResult{}, ErrInvalid
		}
	}
	return result, nil
}

// ReadCloseReplay requires fresh editor/session authorization in SQL. It never
// reads expiring objects or recomputes a previously committed close decision.
func ReadCloseReplay(ctx context.Context, tx pgx.Tx, tenant, intent, actor, session uuid.UUID, key, requestSHA string) (ReplayResult, error) {
	if tx == nil || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil || !idempotency.ValidKey(key) || !shaPattern.MatchString(requestSHA) {
		return ReplayResult{}, ErrInvalid
	}
	var result ReplayResult
	if err := tx.QueryRow(ctx, `SELECT outcome,response_body FROM photo.semantic_close_replay($1,$2,$3,$4,$5,$6)`, tenant, intent, actor, session, key, requestSHA).Scan(&result.Outcome, &result.Body); err != nil {
		return ReplayResult{}, err
	}
	switch result.Outcome {
	case "absent", "pending", "conflict":
		if len(result.Body) != 0 {
			return ReplayResult{}, ErrInvalid
		}
	case "replayed":
		if _, err := DecodeCloseResult(result.Body); err != nil {
			return ReplayResult{}, err
		}
	default:
		return ReplayResult{}, ErrInvalid
	}
	result.Body = append(json.RawMessage(nil), result.Body...)
	return result, nil
}
