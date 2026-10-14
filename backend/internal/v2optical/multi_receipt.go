package v2optical

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

type MultiReceipt struct {
	SchemaVersion int                   `json:"schema_version"`
	Run           uuid.UUID             `json:"run_id"`
	Workflow      uuid.UUID             `json:"workflow_id"`
	Task          uuid.UUID             `json:"task_id"`
	State         string                `json:"state"`
	Actor         uuid.UUID             `json:"actor_id"`
	Session       uuid.UUID             `json:"auth_session_id"`
	Operation     string                `json:"operation"`
	DescriptorSHA string                `json:"descriptor_sha"`
	Qualification MultiQualificationPin `json:"qualification"`
	Transport     json.RawMessage       `json:"transport"`
}

func parseMultiReceipt(ctx context.Context, draft MultiDraft, qualified VerifiedMultiQualification, params v2input.Ref, raw []byte) (MultiReceipt, error) {
	var zero MultiReceipt
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
	var receipt MultiReceipt
	if json.Unmarshal(normal, &receipt) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(receipt)
	if err != nil || !bytes.Equal(normal, exact) {
		return zero, v2input.ErrInvalid
	}
	descriptor, err := draft.Descriptor(ctx, qualified, params)
	if err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(json.RawMessage(descriptor))
	if err != nil {
		return zero, err
	}
	s, err := draft.Snapshot()
	if err != nil {
		return zero, err
	}
	pin, err := qualified.Pin()
	if err != nil {
		return zero, err
	}
	if receipt.SchemaVersion != 1 || receipt.State != "queued" || receipt.Run != s.Run || receipt.Workflow != s.Workflow || receipt.Task != s.Task || receipt.Actor != s.Actor || receipt.Session != s.Session || receipt.Operation != s.Operation || receipt.DescriptorSHA != digest || receipt.Qualification != pin {
		return zero, v2input.ErrInvalid
	}
	if _, err = RestoreMultiTransport(ctx, draft, qualified, params, receipt.Transport); err != nil {
		return zero, err
	}
	receipt.Transport = append(json.RawMessage(nil), receipt.Transport...)
	return receipt, nil
}

// ReadMultiQueuedReceipt returns historical admission acknowledgement. It does
// not publish, reserve quota, activate tasks or report current execution status.
func ReadMultiQueuedReceipt(ctx context.Context, db PerspectiveTransactions, bucket string, who auth.Identity,
	tenant uuid.UUID, draft MultiDraft, qualified VerifiedMultiQualification, params v2input.Ref, located ...LocatedMultiQualification) (MultiReceipt, error) {
	var zero MultiReceipt
	if err := validateMultiLocated(qualified, located); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if _, err := draft.Descriptor(ctx, qualified, params); err != nil {
		return zero, err
	}
	snapshot, err := draft.Snapshot()
	if err != nil {
		return zero, err
	}
	repository := multiParamsRepository{db: db, bucket: bucket, tenant: tenant, who: who, plan: draft}
	scope := artifactjson.Scope{Tenant: tenant, Run: snapshot.Run, Owner: snapshot.Task, Purpose: "task_params"}
	if _, err := repository.proof(scope, snapshot.ParamsSHA, int64(len(snapshot.ParamsJCS))); err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = repository.authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.read_v29_multi_queued_receipt($1,$2,$3,$4,$5::jsonb)", tenant, snapshot.Run, who.UserID, who.SessionID, draft.raw).Scan(&raw); err != nil {
		return zero, err
	}
	receipt, err := parseMultiReceipt(ctx, draft, qualified, params, raw)
	if err != nil {
		return zero, err
	}
	if len(located) == 1 {
		snapshot, err := draft.Snapshot()
		if err != nil {
			return zero, err
		}
		if err := LockLocatedMultiQualification(ctx, tx, who, tenant, snapshot.Project, located[0]); err != nil {
			return zero, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return receipt, nil
}
