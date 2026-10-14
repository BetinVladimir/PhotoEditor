package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
		"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
		"photoeditor/backend/internal/v2input"
	"time"
)

type NegativeReceipt struct {
	SchemaVersion int                         `json:"schema_version"`
	Run           uuid.UUID                   `json:"run_id"`
	Workflow      uuid.UUID                   `json:"workflow_id"`
	Task          uuid.UUID                   `json:"task_id"`
	State         string                      `json:"state"`
	Actor         uuid.UUID                   `json:"actor_id"`
	Session       uuid.UUID                   `json:"auth_session_id"`
	Operation     string                      `json:"operation"`
	DescriptorSHA string                      `json:"descriptor_sha"`
	Qualification NegativeQualificationPin `json:"qualification"`
	Transport     json.RawMessage             `json:"transport"`
}

func parseNegativeReceipt(ctx context.Context, draft NegativeDraft, located LocatedNegativeQualification, published v2input.Ref, raw []byte) (NegativeReceipt, error) {
	var zero NegativeReceipt
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
	var receipt NegativeReceipt
	if json.Unmarshal(normal, &receipt) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(receipt)
	if err != nil || !bytes.Equal(normal, exact) {
		return zero, v2input.ErrInvalid
	}
	qualified, err := located.Qualification()
 if err != nil { return zero,err }
 descriptor, err := draft.Descriptor(ctx, qualified, published)
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
	if receipt.SchemaVersion != 1 || receipt.State != "queued" || receipt.Run != s.Owner.RunID || receipt.Workflow != s.Owner.WorkflowID || receipt.Task != s.Owner.TaskID || receipt.Actor != s.Owner.ActorID || receipt.Session != s.Owner.AuthSessionID || receipt.Operation != "optical.negative" || receipt.DescriptorSHA != digest || receipt.Qualification != pin {
		return zero, v2input.ErrInvalid
	}
	if _, err = draft.RestorePublishedTransport(ctx, receipt.Transport, qualified, published); err != nil {
		return zero, err
	}
	receipt.Transport = append(json.RawMessage(nil), receipt.Transport...)
	return receipt, nil
}

// ReadNegativeQueuedReceipt reads historical admission acknowledgement, never
// current execution status. It has no publication, registration or sealing effects.
func ReadNegativeQueuedReceipt(ctx context.Context, db NegativeTransactions, objects v2input.SessionAnalysisObjectReader, bucket string, who auth.Identity, tenant uuid.UUID, draft NegativeDraft, located LocatedNegativeQualification, published v2input.Ref) (NegativeReceipt,error) {
 var zero NegativeReceipt
 if err:=ctx.Err();err!=nil { return zero,err }
 if db==nil || objects==nil || bucket=="" || len(bucket)>255 { return zero,v2input.ErrInvalid }
 snapshot,err:=draft.Snapshot();if err!=nil { return zero,err }
 if tenant!=snapshot.Owner.TenantID || who.UserID!=snapshot.Owner.ActorID || who.SessionID!=snapshot.Owner.AuthSessionID { return zero,v2input.ErrInvalid }
 qualified,err:=located.Qualification();if err!=nil { return zero,err }
 plan,paramsRef,source,err:=draft.RestorePlan(ctx,qualified);if err!=nil { return zero,err }
 if paramsRef!=published { return zero,v2input.ErrInvalid }
 params,err:=ParseNegativeParams(plan.Params);if err!=nil { return zero,err }
 physical,err:=readNegativeSourceProof(ctx,SQLNegativeSourceArrayCatalog{DB:db,Bucket:bucket},objects,who,tenant,snapshot.Owner.ProjectID,snapshot.Owner.SnapshotID,params,source);if err!=nil { return zero,err }
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil { return zero,err };if tx==nil { return zero,v2input.ErrInvalid }
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err:=lockNegativeSourceProof(ctx,tx,bucket,who,tenant,snapshot.Owner.ProjectID,snapshot.Owner.SnapshotID,params,source,physical);err!=nil { return zero,err }
 if err:=LockLocatedNegativeQualification(ctx,tx,who,tenant,snapshot.Owner.ProjectID,located);err!=nil { return zero,err }
 var raw []byte
 if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_queued_receipt($1,$2,$3,$4,$5)",tenant,snapshot.Owner.RunID,who.UserID,who.SessionID,draft.raw).Scan(&raw);err!=nil { return zero,err }
 receipt,err:=parseNegativeReceipt(ctx,draft,located,published,raw);if err!=nil { return zero,err }
 if err:=tx.Commit(ctx);err!=nil { return zero,err }
 return receipt,nil
}
