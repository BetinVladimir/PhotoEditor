package v2portrait

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

// RelightDraftIntent contains authored intent without client-selected run IDs.
// Project and tenant come from the configured service; identity from middleware.
type RelightDraftIntent struct {
	Snapshot    uuid.UUID
	Ordinal     int
	SubjectKind string
	ParamsRaw   []byte
}

// CreateKeyedDraft derives route-specific IDs and durably binds the full intent.
// The SQL draft configuration detects changed requests under the same key.
// This is preliminary draft idempotency, not an admission response receipt.
func (s *RelightSubmissionService) CreateKeyedDraft(ctx context.Context, who auth.Identity, key string, intent RelightDraftIntent) (RelightSubmissionRequest, error) {
	request, err := s.keyedDraftRequest(ctx, who, key, intent)
	if err != nil {
		return RelightSubmissionRequest{}, err
	}
	if err = s.CreateDraft(ctx, who, request); err != nil {
		return RelightSubmissionRequest{}, err
	}
	return request, nil
}

func (s *RelightSubmissionService) keyedDraftRequest(ctx context.Context, who auth.Identity, key string, intent RelightDraftIntent) (RelightSubmissionRequest, error) {
	var zero RelightSubmissionRequest
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
		return zero, v2input.ErrInvalid
	}
	run := uuid.NewSHA1(s.tenant, []byte("V28.Relight.CreateDraft:"+s.project.String()+":"+who.UserID.String()+":"+key))
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	request := RelightSubmissionRequest{Snapshot: intent.Snapshot, Run: run, Workflow: workflow, Ordinal: intent.Ordinal, SubjectKind: intent.SubjectKind, ParamsRaw: append([]byte(nil), intent.ParamsRaw...)}
	body, err := canonical.Marshal(relightSubmissionWire{SchemaVersion: 1, Snapshot: request.Snapshot, Run: run, Workflow: workflow, Ordinal: request.Ordinal, SubjectKind: request.SubjectKind, Params: request.ParamsRaw})
	if err != nil {
		return zero, err
	}
	return DecodeRelightSubmissionRequest(body)
}

// SubmitKeyed creates or resumes the draft; a previously admitted exact request
// returns its authorized historical receipt without publishing or activating.
func (s *RelightSubmissionService) SubmitKeyed(ctx context.Context, who auth.Identity, key string, intent RelightDraftIntent) ([]byte, error) {
	request, err := s.keyedDraftRequest(ctx, who, key, intent)
	if err != nil {
		return nil, err
	}
	if err = s.CreateDraft(ctx, who, request); err != nil {
		body, encodeErr := canonical.Marshal(relightSubmissionWire{SchemaVersion: 1, Snapshot: request.Snapshot, Run: request.Run, Workflow: request.Workflow, Ordinal: request.Ordinal, SubjectKind: request.SubjectKind, Params: request.ParamsRaw})
		if encodeErr != nil {
			return nil, err
		}
		pin, encodeErr := s.pin.Compile()
		if encodeErr != nil {
			return nil, err
		}
		receipt, recoverErr := s.coordinator.readRelightQueuedReceipt(ctx, who, s.tenant, request.Run, &relightReceiptBinding{project: s.project, request: body, pin: pin})
		if recoverErr != nil {
			return nil, err
		}
		return receipt, nil
	}
	return s.Submit(ctx, who, request)
}
