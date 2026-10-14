package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

type RelightAnalysisReceipt struct {
	SchemaVersion int             `json:"schema_version"`
	Run           uuid.UUID       `json:"run_id"`
	Workflow      uuid.UUID       `json:"workflow_id"`
	Task          uuid.UUID       `json:"task_id"`
	State         string          `json:"state"`
	Actor         uuid.UUID       `json:"actor_id"`
	Session       uuid.UUID       `json:"auth_session_id"`
	Operation     string          `json:"operation"`
	DescriptorSHA string          `json:"descriptor_sha"`
	Qualification v2input.Ref     `json:"qualification"`
	Transport     json.RawMessage `json:"transport"`
}

func parseAnalysisReceipt(ctx context.Context, plan RelightAnalysisPlan, request RelightAnalysisPlanRequest, raw []byte) (RelightAnalysisReceipt, error) {
	var zero RelightAnalysisReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var receipt RelightAnalysisReceipt
	if json.Unmarshal(normal, &receipt) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(receipt)
	if err != nil || !bytes.Equal(normal, exact) {
		return zero, v2input.ErrInvalid
	}
	descriptor, err := plan.Descriptor(ctx)
	if err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(json.RawMessage(descriptor))
	if err != nil {
		return zero, err
	}
	s := plan.Snapshot()
	if receipt.SchemaVersion != 1 || receipt.State != "queued" || receipt.Run != s.Run || receipt.Workflow != s.Workflow || receipt.Task != s.Task || receipt.Actor != request.Actor || receipt.Session != request.Session || receipt.Operation != request.Operation || receipt.DescriptorSHA != digest || receipt.Qualification != request.Runtime.Qualification {
		return zero, v2input.ErrInvalid
	}
	if _, err = plan.RestorePublishedTransport(ctx, receipt.Transport); err != nil {
		return zero, err
	}
	receipt.Transport = append(json.RawMessage(nil), receipt.Transport...)
	return receipt, nil
}

// ReadQueuedReceipt returns historical acknowledgement, never current job
// status or permission to execute. It performs no publication or activation.
func (c RelightAnalysisCoordinator) ReadQueuedReceipt(ctx context.Context, who auth.Identity, request RelightAnalysisPlanRequest) (RelightAnalysisReceipt, error) {
	var zero RelightAnalysisReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || who.UserID != request.Actor || who.SessionID != request.Session {
		return zero, v2input.ErrInvalid
	}
	plan, err := CompileRelightAnalysisPlan(ctx, request)
	if err != nil {
		return zero, err
	}
	expected, err := canonical.Marshal(plan.Snapshot())
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
	if err = (relightAnalysisParamsRepository{}).authorize(ctx, tx, artifactjson.Scope{Tenant: request.Tenant}); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.read_v28_relight_analysis_queued_receipt($1,$2,$3,$4,$5::jsonb)", request.Tenant, plan.snapshot.Run, who.UserID, who.SessionID, string(expected)).Scan(&raw); err != nil {
		return zero, err
	}
	receipt, err := parseAnalysisReceipt(ctx, plan, request, raw)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return receipt, nil
}
