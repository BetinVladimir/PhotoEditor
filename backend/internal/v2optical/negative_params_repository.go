package v2optical

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type negativeParamsRepository struct {
	db     NegativeTransactions
	bucket string
	tenant uuid.UUID
	who    auth.Identity
	plan   NegativeDraft
 located LocatedNegativeQualification
}

func (r negativeParamsRepository) proof(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (string, error) {
 if err:=ctx.Err();err!=nil{return "",err}
 p,err:=r.plan.Snapshot();if err!=nil{return "",err}
 if r.db==nil||len(r.bucket)<3||len(r.bucket)>63||scope.Tenant!=r.tenant||scope.Run!=p.Owner.RunID||scope.Owner!=p.Owner.TaskID||scope.Purpose!="task_params"||scope.AssemblyLease!=uuid.Nil||scope.LocalPortrait||scope.DonorPortrait||sha!=p.ParamsSHA{return "",v2input.ErrInvalid}
 if p.Owner.TenantID!=r.tenant||p.Owner.ActorID!=r.who.UserID||p.Owner.AuthSessionID!=r.who.SessionID||r.who.UserID==uuid.Nil||r.who.SessionID==uuid.Nil{return "",v2input.ErrInvalid}
 qualified,err:=r.located.Qualification();if err!=nil{return "",err}
 plan,_,_,err:=r.plan.RestorePlan(ctx,qualified);if err!=nil{return "",err}
 if size!=int64(len(plan.Params)){return "",v2input.ErrInvalid}
 objects,err:=r.located.Objects();if err!=nil{return "",err}
 raw,err:=canonical.Marshal(map[string]any{"draft_jcs":r.plan.raw,"qualification_objects":objects})
 return string(raw),err
}
func (r negativeParamsRepository) authorize(ctx context.Context, tx pgx.Tx, scope artifactjson.Scope) error {
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	_, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", scope.Tenant.String())
	return err
}
func (r negativeParamsRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var zero artifactjson.Reservation
	proof, err := r.proof(ctx, scope, sha, size)
	if err != nil {
		return zero, err
	}
	if r.db == nil {
		return zero, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = r.authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err = tx.QueryRow(ctx, "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v29_negative_params_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, sha, size, r.bucket, proof).Scan(&outcome, &id, &token, &ready)
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
	planSnapshot, err := r.plan.Snapshot()
	if err != nil {
		return zero, err
	}
	if *id != planSnapshot.ParamsDocument {
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

func (r negativeParamsRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	proof, err := r.proof(ctx, scope, object.SHA256, object.Bytes)
	if err != nil {
		return zero, err
	}
	planSnapshot, err := r.plan.Snapshot()
	if err != nil {
		return zero, err
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || object.Key != key || ticket.Token == uuid.Nil || ticket.ArtifactID != planSnapshot.ParamsDocument || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || r.db == nil {
		return zero, objectstore.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = r.authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	var done bool
	err = tx.QueryRow(ctx, "SELECT photo.complete_v29_negative_params_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.bucket, object.VersionID, proof).Scan(&done)
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

// PublishNegativeParams saves the exact draft before reservation/immutable IO,
// then repeats stored-owner, source and exact qualification locator authority
// when completing. It never creates a runnable task or seals a graph.
func PublishNegativeParams(ctx context.Context,db NegativeTransactions,objects artifactjson.Objects,bucket string,who auth.Identity,tenant uuid.UUID,plan NegativeDraft,located LocatedNegativeQualification)(artifactjson.Ref,error){
 if err:=ctx.Err();err!=nil{return artifactjson.Ref{},err}
 if objects==nil||len(bucket)<3||len(bucket)>63{return artifactjson.Ref{},v2input.ErrInvalid}
 if err:=SaveNegativeDraft(ctx,db,who,tenant,plan,located);err!=nil{return artifactjson.Ref{},err}
 p,err:=plan.Snapshot();if err!=nil{return artifactjson.Ref{},err}
 qualified,err:=located.Qualification();if err!=nil{return artifactjson.Ref{},err}
 compiled,_,_,err:=plan.RestorePlan(ctx,qualified);if err!=nil{return artifactjson.Ref{},err}
 scope:=artifactjson.Scope{Tenant:tenant,Run:p.Owner.RunID,Owner:p.Owner.TaskID,Purpose:"task_params"}
 return (artifactjson.Publisher{Repository:negativeParamsRepository{db:db,bucket:bucket,tenant:tenant,who:who,plan:plan,located:located},Objects:objects}).Publish(ctx,scope,compiled.Params)
}

var _ artifactjson.Repository=negativeParamsRepository{}
