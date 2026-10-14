package v2optical

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2qualification"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type negativeReplacementTransportRepository struct {
 params negativeReplacementParamsRepository
 body string
}

func(r negativeReplacementTransportRepository)proof(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(string,error){
 if err:=ctx.Err();err!=nil{return "",err}
 snapshot,err:=r.params.documentSnapshot(ctx);if err!=nil{return "",err}
 if scope.Tenant!=snapshot.Owner.TenantID||scope.Run!=snapshot.Owner.RunID||scope.Owner!=snapshot.Owner.TaskID||scope.AssemblyLease!=uuid.Nil||scope.LocalPortrait||scope.DonorPortrait||size!=int64(len(r.body))||size<2||size>1<<20||(scope.Purpose!="v2_input_chunk_0"&&scope.Purpose!="v2_input_manifest"){return "",v2input.ErrInvalid}
 sum:=sha256.Sum256([]byte(r.body));if sha!=hex.EncodeToString(sum[:]){return "",v2input.ErrInvalid}
 _,plan,err:=r.params.document(ctx);if err!=nil{return "",err}
 paramsScope:=artifactjson.Scope{Tenant:scope.Tenant,Run:scope.Run,Owner:scope.Owner,Purpose:"task_params"}
 return r.params.proof(ctx,paramsScope,snapshot.Identity.ParamsSHA,int64(len(plan.Params)))
}

func (r negativeReplacementTransportRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var zero artifactjson.Reservation
	proof, err := r.proof(ctx,scope, sha, size)
	if err != nil {
		return zero, err
	}
	if r.params.db == nil {
		return zero, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := r.params.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = r.params.authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	if err=r.params.lock(ctx,tx);err!=nil{return zero,err}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err = tx.QueryRow(ctx, "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v29_negative_replacement_transport_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, sha, size, r.params.bucket, proof, r.body).Scan(&outcome, &id, &token, &ready)
	if err != nil {
		return zero, err
	}
	switch outcome {
	case "quota":
		return zero, artifactjson.ErrQuota
	case "conflict":
		return zero, artifactjson.ErrConflict
	case "forbidden", "stale":
		return zero, artifactjson.ErrStale
	case "created", "resumed", "replayed":
	default:
		return zero, objectstore.ErrIntegrity
	}
	if id == nil || *id == uuid.Nil || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
		return zero, objectstore.ErrIntegrity
	}
	result := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &artifactjson.Ref{ID: *id, SHA256: sha}
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}

func (r negativeReplacementTransportRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	proof, err := r.proof(ctx,scope, object.SHA256, object.Bytes)
	if err != nil {
		return zero, err
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || object.Key != key || ticket.Token == uuid.Nil || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || r.params.db == nil {
		return zero, objectstore.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := r.params.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = r.params.authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	if err=r.params.lock(ctx,tx);err!=nil{return zero,err}
	var done bool
	err = tx.QueryRow(ctx, "SELECT photo.complete_v29_negative_replacement_transport_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.params.bucket, object.VersionID, proof, r.body).Scan(&done)
	if err != nil {
		return zero, err
	}
	if !done {
		return zero, artifactjson.ErrStale
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return artifactjson.Ref{ID: ticket.ArtifactID, SHA256: object.SHA256}, nil
}

type negativeReplacementTransportDocuments struct {
 params negativeReplacementParamsRepository
 objects artifactjson.Objects
}
func(p negativeReplacementTransportDocuments)Publish(ctx context.Context,scope artifactjson.Scope,body []byte)(artifactjson.Ref,error){
 repository:=negativeReplacementTransportRepository{params:p.params,body:string(body)}
 return (artifactjson.Publisher{Repository:repository,Objects:p.objects}).Publish(ctx,scope,body)
}

// PublishNegativeReplacementTaskTransport stages the blocked task before IO.
// Every document phase repeats current whole-wave physical and qualification
// locks; SQL compares chunk/manifest content with the exact staged descriptor.
// Input registration and graph admission remain separate dedicated operations.
func(p PreparedNegativeReplacementInputs)PublishNegativeReplacementTaskTransport(ctx context.Context,db NegativeTransactions,objects artifactjson.Objects,bucket string,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft NegativeReplacementDraft,snapshot uuid.UUID,located LocatedNegativeQualification,locks v2qualification.DeploymentLocks,capacity NegativeResourceEstimate,published v2input.Ref)(v2input.GraphTask,error){
 var zero v2input.GraphTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if objects==nil{return zero,v2input.ErrInvalid}
 if err:=p.StageNegativeReplacementTask(ctx,db,bucket,rebuilt,retained,mappings,unchanged,draft,snapshot,located,locks,capacity,published);err!=nil{return zero,err}
 repository:=negativeReplacementParamsRepository{db:db,bucket:bucket,prepared:p,draft:draft,snapshot:snapshot,located:located,locks:locks,capacity:capacity,rebuilt:append([]VerifiedAdoptedParentArrays(nil),rebuilt...),retained:append([]VerifiedAdoptedParentArrays(nil),retained...),mappings:append([]VerifiedNegativeAffectedSources(nil),mappings...),unchanged:append([]VerifiedNegativeUnchangedArrays(nil),unchanged...)}
 saved,plan,err:=repository.document(ctx);if err!=nil{return zero,err}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 _,params,source,err:=RestoreOwnedNegativeRecovery(ctx,[]byte(saved.RecoveryJCS),saved.Owner,qualified);if err!=nil{return zero,err}
 if params!=published{return zero,v2input.ErrInvalid}
 documents:=negativeReplacementTransportDocuments{params:repository,objects:objects}
 publisher:=v2input.ScopedPublisher{Documents:documents,Tenant:p.tenant,Run:saved.Owner.RunID}
 return BuildNegativeTaskTransport(ctx,saved.Owner.TaskID,plan,source,qualified,published,publisher)
}
var _ artifactjson.Repository=negativeReplacementTransportRepository{}
