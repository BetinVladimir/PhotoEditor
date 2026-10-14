package delivery

import (
	"context"
 "bytes"
 "photoeditor/backend/internal/canonical"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// StageWatermarkCreateTransport publishes chunks and manifest after the blocked task and
// exact creation documents commit. documents must be configured with the trusted
// scheduler repository; this method does not widen generic SQL rights for API.
// Every retry resumes the same durable per-task/per-ordinal reservations.
func (s Service) StageWatermarkCreateTransport(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key, bucket string, objects CreatePreparationObjects, documents v2input.DocumentPublisher, limits PrepareResourceLimits, metadata ...*PythonMetadataSourcePreparer) (StagedCreateTransport, error) {
	if err := ctx.Err(); err != nil {
		return StagedCreateTransport{}, err
	}
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return StagedCreateTransport{}, err
	}
	if documents == nil {
		return StagedCreateTransport{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	request = intent.Request()
 recovery,err:=s.RecoverWatermarkSelection(ctx,who,tid,request,key);if err!=nil{return StagedCreateTransport{},err}
 if !recovery.Found||recovery.Completed{return StagedCreateTransport{},domain.NewError("DELIVERY_CREATE_STALE",409)}
 raw,err:=canonical.Marshal(recovery.Pending);if err!=nil{return StagedCreateTransport{},err}
 frozen, err := s.StageWatermarkCreatePrepare(ctx, who, tid, request, key,raw, bucket, objects, limits, metadata...)
	if err != nil {
		return StagedCreateTransport{}, err
	}
	graph, err := frozen.PublishInputs(ctx, v2input.ScopedPublisher{Documents: documents, Tenant: tid, Run: intent.identity.Run})
	if err != nil {
		return StagedCreateTransport{}, err
	}
	graph, err = frozen.BindPublishedInputs(ctx, graph)
	if err != nil {
		return StagedCreateTransport{}, err
	}
	// Revoke/expiry during object IO must not escape as a usable stage result.
	current, err := s.RecoverWatermarkSelection(ctx, who, tid, request, key)
	if err != nil {
		return StagedCreateTransport{}, err
	}
	if !current.Found || current.Completed {
		return StagedCreateTransport{}, domain.NewError("DELIVERY_CREATE_STALE", 409)
	}
 currentRaw,err:=canonical.Marshal(current.Pending);if err!=nil{return StagedCreateTransport{},err}
 if !bytes.Equal(raw,currentRaw){return StagedCreateTransport{},domain.NewError("DELIVERY_SOURCE_STALE",409)}
 if _,err=s.StageWatermarkSelectionSources(ctx,who,tid,request,key,currentRaw,bucket,objects);err!=nil{return StagedCreateTransport{},err}
 return StagedCreateTransport{Frozen: frozen, Graph: graph}, ctx.Err()
}
