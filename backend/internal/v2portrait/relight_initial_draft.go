package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

// CreateDraft persists an authorized preliminary run without Params, tasks,
// reservation or activation. Reusing IDs with changed intent is refused.
func (s *RelightSubmissionService) CreateDraft(ctx context.Context, who auth.Identity, request RelightSubmissionRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	body, err := canonical.Marshal(relightSubmissionWire{SchemaVersion: 1, Snapshot: request.Snapshot, Run: request.Run, Workflow: request.Workflow, Ordinal: request.Ordinal, SubjectKind: request.SubjectKind, Params: request.ParamsRaw})
	if err != nil {
		return err
	}
	if _, err = DecodeRelightSubmissionRequest(body); err != nil {
		return err
	}
	workflow, err := v2input.WorkflowID(request.Run)
	if err != nil || workflow != request.Workflow {
		return v2input.ErrInvalid
	}
	pin, err := s.pin.Compile()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.coordinator.DB.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer tx.Rollback(context.Background())
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", s.tenant.String()); err != nil {
		return err
	}
	var queue *string
 if err = tx.QueryRow(ctx, "SELECT photo.v32_relight_initial_queue($1,$2,$3,$4,$5)", s.tenant, s.project, who.UserID, who.SessionID, request.Run).Scan(&queue); err != nil { return err }
 if queue == nil { return v2input.ErrExpired }
 config, err := compileRelightInitialConfig(s.project, who.SessionID, body, pin, *queue)
 if err != nil { return err }
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT photo.create_v28_relight_initial_draft($1,$2,$3,$4,$5,$6::jsonb,$7)", s.tenant, s.project, who.UserID, who.SessionID, string(body), string(pin), string(config)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return tx.Commit(ctx)
}

func compileRelightInitialConfig(project, session uuid.UUID, request, pin []byte, queue string) ([]byte,error) {
 if queue!=""&&queue!="interactive" { return nil,v2input.ErrInvalid }
 value:=map[string]any{"project_id":project,"v28_relight_request":json.RawMessage(request),"v28_relight_pin":json.RawMessage(pin),"auth_session_id":session}
 if queue!="" { value["queue_class"]=queue }
 return canonical.Marshal(value)
}
