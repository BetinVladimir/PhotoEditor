package delivery

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
)

// StageWatermarkCreatePrepare resumes a previously pinned creation request through
// source resolution, draft run creation, physical document publication and
// blocked prepare materialization. Object IO never occurs in a SQL transaction.
// Caller supplies already persisted selection bytes; pin selection and physical
// verification commit before publication. Retry reuses exact document identities.
// The task remains blocked: this is not completed CreateDelivery admission.
func (s Service) StageWatermarkCreatePrepare(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, pendingRaw []byte, bucket string, objects CreatePreparationObjects, limits PrepareResourceLimits, metadata ...*PythonMetadataSourcePreparer) (FrozenPrepareTask, error) {
	if err := ctx.Err(); err != nil {
		return FrozenPrepareTask{}, err
	}
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	if s.DB == nil || len(bucket) < 3 || len(bucket) > 63 || objects == nil {
		return FrozenPrepareTask{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	if len(metadata)>1 { return FrozenPrepareTask{}, domain.NewError("DELIVERY_METADATA_UNAVAILABLE", 503) }
	if request.Policy.MetadataPolicy != "strip" {
		if len(metadata)!=1 || metadata[0]==nil { return FrozenPrepareTask{}, domain.NewError("DELIVERY_METADATA_UNAVAILABLE", 503) }
		if _, err := metadata[0].environment(); err != nil { return FrozenPrepareTask{}, domain.NewError("DELIVERY_METADATA_UNAVAILABLE", 503) }
	}
	// Use the owned request throughout; public slice fields may otherwise be
	// mutated by a caller while asynchronous IO is running.
	request = intent.Request()
 pendingRaw,err=intent.ownWatermarkSelectionPending(ctx,who.SessionID,pendingRaw);if err!=nil{return FrozenPrepareTask{},err}
 pins,err:=s.StageWatermarkSelectionSources(ctx,who,tid,request,key,pendingRaw,bucket,objects);if err!=nil{return FrozenPrepareTask{},err}
 pending,err:=intent.DecodeWatermarkPending(ctx,who.SessionID,pendingRaw,pins);if err!=nil{return FrozenPrepareTask{},err}
	raw, err := canonical.Marshal(pending)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	frozen, err := intent.ResolveWatermarkPrepareTask(ctx, who, raw, SQLCreateRecipeInputResolver{DB: s.DB, Bucket: bucket, Objects: objects}, limits,pins)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	docs, err := intent.CompileWatermarkDocuments(ctx, who.SessionID, pending.Catalog, pending.Items,pins)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return intent.BeginWatermarkSelectionRun(ctx, tx, who, tid, pending)
	})
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	publisher := CreateStoragePublisher{
		Tenant: tid, Objects: objects,
		Store: SQLCreateDocumentStore{DB: s.DB, Intent: intent, Who: who, Bucket: bucket},
	}
	if err := intent.PublishDocuments(ctx, docs, publisher); err != nil {
		return FrozenPrepareTask{}, err
	}
	if request.Policy.MetadataPolicy != "strip" {
		if err := s.prepareAndPinCreateMetadata(ctx, who, tid, intent, docs, pending.Items, bucket, objects, *metadata[0]); err != nil { return FrozenPrepareTask{}, err }
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return intent.MaterializePrepare(ctx, tx, who, tid, frozen)
	})
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	return frozen, nil
}
