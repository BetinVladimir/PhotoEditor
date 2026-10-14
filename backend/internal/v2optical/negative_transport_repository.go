package v2optical

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type negativeTransportRepository struct {
 params negativeParamsRepository
 body string
}

func(r negativeTransportRepository)proof(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(string,error){
 if err:=ctx.Err();err!=nil{return "",err}
 snapshot,err:=r.params.plan.Snapshot();if err!=nil{return "",err}
 if scope.Tenant!=snapshot.Owner.TenantID||scope.Run!=snapshot.Owner.RunID||scope.Owner!=snapshot.Owner.TaskID||scope.AssemblyLease!=uuid.Nil||scope.LocalPortrait||scope.DonorPortrait||size!=int64(len(r.body))||size<2||size>1<<20||(scope.Purpose!="v2_input_chunk_0"&&scope.Purpose!="v2_input_manifest"){return "",v2input.ErrInvalid}
 sum:=sha256.Sum256([]byte(r.body));if sha!=hex.EncodeToString(sum[:]){return "",v2input.ErrInvalid}
 qualified,err:=r.params.located.Qualification();if err!=nil{return "",err}
 plan,_,_,err:=r.params.plan.RestorePlan(ctx,qualified);if err!=nil{return "",err}
 paramsScope:=artifactjson.Scope{Tenant:scope.Tenant,Run:scope.Run,Owner:scope.Owner,Purpose:"task_params"}
 return r.params.proof(ctx,paramsScope,snapshot.ParamsSHA,int64(len(plan.Params)))
}

func (r negativeTransportRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
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
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err = tx.QueryRow(ctx, "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v29_negative_transport_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, sha, size, r.params.bucket, proof, r.body).Scan(&outcome, &id, &token, &ready)
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

func (r negativeTransportRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
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
	var done bool
	err = tx.QueryRow(ctx, "SELECT photo.complete_v29_negative_transport_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.params.bucket, object.VersionID, proof, r.body).Scan(&done)
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

type negativeTransportDocuments struct {
 params negativeParamsRepository
 objects artifactjson.Objects
}
func(p negativeTransportDocuments)Publish(ctx context.Context,scope artifactjson.Scope,body []byte)(artifactjson.Ref,error){
 repository:=negativeTransportRepository{params:p.params,body:string(body)}
 return (artifactjson.Publisher{Repository:repository,Objects:p.objects}).Publish(ctx,scope,body)
}

// PublishNegativeTaskTransport stages before immutable chunk/manifest IO. SQL
// rechecks the stored descriptor and live source/qualification/quota for every
// reserve/complete. This does not register inputs or admit runnable work.
func PublishNegativeTaskTransport(ctx context.Context,db NegativeTransactions,objects artifactjson.Objects,bucket string,who auth.Identity,tenant uuid.UUID,draft NegativeDraft,located LocatedNegativeQualification,published v2input.Ref)(v2input.GraphTask,error){
 var zero v2input.GraphTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if objects==nil{return zero,v2input.ErrInvalid}
 if err:=StageNegativeTask(ctx,db,bucket,who,tenant,draft,located,published);err!=nil{return zero,err}
 snapshot,err:=draft.Snapshot();if err!=nil{return zero,err}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 plan,params,source,err:=draft.RestorePlan(ctx,qualified);if err!=nil{return zero,err}
 if params!=published{return zero,v2input.ErrInvalid}
 repository:=negativeParamsRepository{db:db,bucket:bucket,tenant:tenant,who:who,plan:draft,located:located}
 documents:=negativeTransportDocuments{params:repository,objects:objects}
 publisher:=v2input.ScopedPublisher{Documents:documents,Tenant:tenant,Run:snapshot.Owner.RunID}
 return BuildNegativeTaskTransport(ctx,snapshot.Owner.TaskID,plan,source,qualified,published,publisher)
}

var _ artifactjson.Repository=negativeTransportRepository{}
