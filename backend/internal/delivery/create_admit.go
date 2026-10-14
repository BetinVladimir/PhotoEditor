package delivery

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

// AdmitPreparedPackage belongs in the caller's tenant transaction after task
// input registration. The caller must roll back on any error. SQL independently
// requires ready documents, the blocked task and its registered source inputs.
// This method does not seal the graph, dispatch work or complete idempotency.
func (i CreateIntent) AdmitPreparedPackage(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID, pendingRaw []byte) error {
 return i.admitPreparedPackage(ctx,tx,who,tid,pendingRaw,nil,false)
}
// AdmitWatermarkPreparedPackage repeats immutable pin compilation and SQL
// source authorization in the caller's transaction. It does not activate work.
func(i CreateIntent)AdmitWatermarkPreparedPackage(ctx context.Context,tx ProofTransaction,who auth.Identity,tid uuid.UUID,pendingRaw []byte,pins []PreparedWatermarkPin)error{
 return i.admitPreparedPackage(ctx,tx,who,tid,pendingRaw,pins,true)
}
func(i CreateIntent)admitPreparedPackage(ctx context.Context,tx ProofTransaction,who auth.Identity,tid uuid.UUID,pendingRaw []byte,pins []PreparedWatermarkPin,watermark bool)error{
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tid != i.tenant || who.UserID != i.actor || who.SessionID == uuid.Nil || i.identity.Run == uuid.Nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	pending, err := i.decodePending(ctx, who.SessionID, pendingRaw,pins,watermark)
	if err != nil {
		return err
	}
	docs, err := i.compileDocuments(ctx, who.SessionID, pending.Catalog, pending.Items,pins,watermark)
	if err != nil {
		return err
	}
	configs := make([]PresetContent, len(docs.Presets))
	for index, doc := range docs.Presets {
		preset := pending.Catalog.Presets[index]
		configs[index] = PresetContent{ID: preset.ID, SHA: preset.SHA256, Ref: doc.Ref, Raw: doc.Body}
	}
 scope:=PrepareProofScope{Tenant:tid,Project:i.request.ProjectID,Run:i.identity.Run,Task:i.identity.PrepareTask,Params:docs.Params.Ref,Frozen:docs.Frozen.Ref}
 if watermark{err=SaveWatermarkPrepareProof(ctx,tx,who,scope,docs.Frozen.Body,docs.Snapshot.Body,configs,pins)}else{err=SavePrepareProof(ctx,tx,who,scope,docs.Frozen.Body,docs.Snapshot.Body,configs)}
 if err!=nil{return err}
	return docs.Plan.AdmitPackage(ctx, tx, who, PackageAdmission{
		Tenant: tid, Package: i.identity.Package, Workflow: i.identity.Workflow,
		Name: i.request.Name, Policy: docs.Policy.Ref,
	})
}
