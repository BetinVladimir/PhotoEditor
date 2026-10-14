package v2optical

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"encoding/json"
 "photoeditor/backend/internal/v2qualification"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type manualPerspectiveReplacementParamsRepository struct {
 db NegativeTransactions
 bucket string
 prepared PreparedNegativeReplacementInputs
 draft PerspectiveReplacementDraft
 snapshot uuid.UUID
 located LocatedPerspectiveQualification
 locks v2qualification.DeploymentLocks
 maxPixels int64
 capacity PerspectiveResourceEstimate
 rebuilt,retained []VerifiedAdoptedParentArrays
 mappings []VerifiedNegativeAffectedSources
 unchanged []VerifiedNegativeUnchangedArrays
}

// document derives publication bytes only from a freshly restored complete wave.
func(r manualPerspectiveReplacementParamsRepository)document(ctx context.Context)(perspectiveReplacementDraftSnapshot,PerspectiveTaskPlan,error){
 var zero perspectiveReplacementDraftSnapshot
 if err:=ctx.Err();err!=nil{return zero,PerspectiveTaskPlan{},err}
 qualified,err:=r.located.Qualification();if err!=nil{return zero,PerspectiveTaskPlan{},err}
 pin,err:=qualified.Pin();if err!=nil{return zero,PerspectiveTaskPlan{},err}
 ready,err:=r.prepared.PrepareCompleteMappedSources(ctx,r.rebuilt,r.retained,r.mappings,r.unchanged);if err!=nil{return zero,PerspectiveTaskPlan{},err}
 matched:=0
 for _,step:=range ready{
  if step.parents.step.intent.locator.Operation!="optical.perspective"{continue}
  if _,err:=step.RestorePerspectiveReplacementDraft(ctx,r.draft.Bytes(),r.snapshot,pin,r.locks,qualified.reportRaw,qualified.calibrationRaw,r.maxPixels,r.capacity,nil);err==nil{matched++}
  if err:=ctx.Err();err!=nil{return zero,PerspectiveTaskPlan{},err}
 }
 if matched!=1||json.Unmarshal(r.draft.Bytes(),&zero)!=nil{return perspectiveReplacementDraftSnapshot{},PerspectiveTaskPlan{},v2input.ErrInvalid}
 params,err:=ParsePerspectiveParams([]byte(zero.Draft.ParamsJCS));if err!=nil{return perspectiveReplacementDraftSnapshot{},PerspectiveTaskPlan{},err}
 if params.Mode!="manual"||zero.Draft.LinesJCS!=""{return perspectiveReplacementDraftSnapshot{},PerspectiveTaskPlan{},v2input.ErrInvalid}
 plan,err:=CompilePerspectiveTaskPlan(ctx,params.Base,[]byte(zero.Draft.ParamsJCS),[]byte(zero.Draft.SourceJCS),qualified,r.maxPixels,r.capacity)
 if err!=nil{return perspectiveReplacementDraftSnapshot{},PerspectiveTaskPlan{},err}
 if plan.ParamsSHA!=zero.Identity.ParamsSHA{return perspectiveReplacementDraftSnapshot{},PerspectiveTaskPlan{},v2input.ErrInvalid}
 return zero,plan,nil
}
func(r manualPerspectiveReplacementParamsRepository)proof(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(string,error){
 if err:=ctx.Err();err!=nil{return "",err}
 p,plan,err:=r.document(ctx);if err!=nil{return "",err}
 if r.db==nil||len(r.bucket)<3||len(r.bucket)>63||scope.Tenant!=r.prepared.tenant||scope.Run!=p.Identity.Run||scope.Owner!=p.Identity.Task||scope.Purpose!="task_params"||scope.AssemblyLease!=uuid.Nil||scope.LocalPortrait||scope.DonorPortrait||sha!=p.Identity.ParamsSHA||size!=int64(len(plan.Params)){return "",v2input.ErrInvalid}
 objects,err:=r.located.Objects();if err!=nil{return "",err}
 raw,err:=canonical.Marshal(map[string]any{"draft_jcs":r.draft.raw,"invalidation_plan":json.RawMessage(r.prepared.planJCS),"qualification_objects":objects})
 return string(raw),err
}
func(r manualPerspectiveReplacementParamsRepository)lock(ctx context.Context,tx pgx.Tx)error{
 _,err:=r.prepared.LockManualPerspectiveReplacementDraft(ctx,tx,r.rebuilt,r.retained,r.mappings,r.unchanged,r.draft.Bytes(),r.snapshot,r.located,r.locks,r.maxPixels,r.capacity)
 return err
}

func (r manualPerspectiveReplacementParamsRepository) authorize(ctx context.Context, tx pgx.Tx, scope artifactjson.Scope) error {
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
func (r manualPerspectiveReplacementParamsRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
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
	if err=r.lock(ctx,tx);err!=nil{return zero,err}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err = tx.QueryRow(ctx, "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v29_manual_perspective_replacement_params_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, sha, size, r.bucket, proof).Scan(&outcome, &id, &token, &ready)
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
	planSnapshot, err := r.documentSnapshot(ctx)
	if err != nil {
		return zero, err
	}
	if *id != planSnapshot.Identity.ParamsDocument {
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

func (r manualPerspectiveReplacementParamsRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	proof, err := r.proof(ctx, scope, object.SHA256, object.Bytes)
	if err != nil {
		return zero, err
	}
	planSnapshot, err := r.documentSnapshot(ctx)
	if err != nil {
		return zero, err
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || object.Key != key || ticket.Token == uuid.Nil || ticket.ArtifactID != planSnapshot.Identity.ParamsDocument || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || r.db == nil {
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
	if err=r.lock(ctx,tx);err!=nil{return zero,err}
	var done bool
	err = tx.QueryRow(ctx, "SELECT photo.complete_v29_manual_perspective_replacement_params_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.bucket, object.VersionID, proof).Scan(&done)
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

func(r manualPerspectiveReplacementParamsRepository)documentSnapshot(ctx context.Context)(perspectiveReplacementDraftSnapshot,error){
 snapshot,_,err:=r.document(ctx);return snapshot,err
}

// PublishManualPerspectiveReplacementParams saves preliminary draft before object IO and
// repeats complete source/qualification locks at reservation and completion.
// It does not register tasks, reserve task outputs or seal the replacement run.
func(p PreparedNegativeReplacementInputs)PublishManualPerspectiveReplacementParams(ctx context.Context,db NegativeTransactions,objects artifactjson.Objects,bucket string,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft PerspectiveReplacementDraft,snapshot uuid.UUID,located LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity PerspectiveResourceEstimate)(artifactjson.Ref,error){
 var zero artifactjson.Ref
 if err:=ctx.Err();err!=nil{return zero,err}
 if objects==nil||bucket!=p.bucket||len(bucket)<3||len(bucket)>63{return zero,v2input.ErrInvalid}
 r:=manualPerspectiveReplacementParamsRepository{db:db,bucket:bucket,prepared:p,draft:draft,snapshot:snapshot,located:located,locks:locks,maxPixels:maxPixels,capacity:capacity,rebuilt:append([]VerifiedAdoptedParentArrays(nil),rebuilt...),retained:append([]VerifiedAdoptedParentArrays(nil),retained...),mappings:append([]VerifiedNegativeAffectedSources(nil),mappings...),unchanged:append([]VerifiedNegativeUnchangedArrays(nil),unchanged...)}
 saved,plan,err:=r.document(ctx);if err!=nil{return zero,err}
 if err=p.SaveManualPerspectiveReplacementDraft(ctx,db,r.rebuilt,r.retained,r.mappings,r.unchanged,draft,snapshot,located,locks,maxPixels,capacity);err!=nil{return zero,err}
 scope:=artifactjson.Scope{Tenant:p.tenant,Run:saved.Identity.Run,Owner:saved.Identity.Task,Purpose:"task_params"}
 return (artifactjson.Publisher{Repository:r,Objects:objects}).Publish(ctx,scope,plan.Params)
}
var _ artifactjson.Repository=manualPerspectiveReplacementParamsRepository{}
