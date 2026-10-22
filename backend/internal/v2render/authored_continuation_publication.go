package v2render

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type AuthoredContinuationPublication struct {
 Scheduler *pgxpool.Pool
 PhaseObjects SQLAuthoredPhaseObjects
 Objects artifactjson.Objects
 Bucket string
}

// Publish compiles from the actual locked ledger and immutable version reads.
// SQL independently rebuilds the continuation from current canonical phases,
// reserves real quota, and rechecks that same authority at physical completion.
// No caller supplies receipt bodies, future output IDs or postcommit leases.
func(p AuthoredContinuationPublication)Publish(ctx context.Context,who auth.Identity,tenant,parent uuid.UUID,ordinal int)(v2input.Ref,CompiledAuthoredFinalParams,error){
 invalid:=func(err error)(v2input.Ref,CompiledAuthoredFinalParams,error){return v2input.Ref{},CompiledAuthoredFinalParams{},err}
 if p.Scheduler==nil||p.Objects==nil||p.Bucket==""||p.PhaseObjects.Bucket!=p.Bucket{return invalid(v2input.ErrInvalid)}
 documents,found,err:=ReadAuthoredFinalDocuments(ctx,p.Scheduler,p.PhaseObjects,who,tenant,parent,ordinal)
 if err!=nil{return invalid(err)};if !found{return invalid(artifactjson.ErrStale)}
 body,sha,_,err:=CompileAuthoredFinalContinuation(documents);if err!=nil{return invalid(err)}
 compounds:=[]string{}
 for _,phase:=range documents.Inventory.Phases{
  if phase.Kind!="geometry"{continue};var receipt AuthoredPhaseReceipt
  if decodeAuthoredParams([]byte(phase.ReceiptJCS),&receipt)!=nil{return invalid(v2input.ErrInvalid)}
  raw,ok:=documents.Bodies[receipt.Result];if !ok{return invalid(v2input.ErrInvalid)};compounds=append(compounds,string(raw))
 }
 witnesses,err:=canonical.Marshal(compounds);if err!=nil{return invalid(err)}
 scope:=artifactjson.Scope{Tenant:tenant,Run:parent,Owner:documents.Inventory.Context.Instruction.Identity,Purpose:"v26_authored_continuation"}
 repository:=authoredContinuationDocuments{DB:p.Scheduler,scope:scope,ordinal:ordinal,bucket:p.Bucket,body:string(body),sha:sha,witnesses:string(witnesses)}
 published,err:=(artifactjson.Publisher{Repository:repository,Objects:p.Objects}).Publish(ctx,scope,body)
 if err!=nil{return invalid(err)}
 after,found,err:=ReadAuthoredFinalInventory(ctx,p.Scheduler,tenant,parent,ordinal);if err!=nil{return invalid(err)};if !found{return invalid(artifactjson.ErrStale)}
 beforeRaw,err:=canonical.Marshal(documents.Inventory);if err!=nil{return invalid(err)};afterRaw,err:=canonical.Marshal(after)
 if err!=nil{return invalid(err)};if !bytes.Equal(beforeRaw,afterRaw){return invalid(artifactjson.ErrStale)}
 ref:=v2input.Ref{ArtifactID:published.ID,SHA256:published.SHA256}
 params,err:=MaterializeAuthoredFinalParams(documents,ref);if err!=nil{return invalid(err)}
 if err=ctx.Err();err!=nil{return invalid(err)};return ref,params,nil
}

type authoredContinuationDocuments struct {
 DB artifactjson.RowQuerier
 scope artifactjson.Scope
 ordinal int
 bucket,body,sha,witnesses string
}
func(r authoredContinuationDocuments)Reserve(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 if r.DB==nil||scope!=r.scope||sha!=r.sha||size!=int64(len(r.body))||!json.Valid([]byte(r.witnesses)){return artifactjson.Reservation{},objectstore.ErrInvalid}
 var id,token uuid.UUID;var ready bool
 err:=r.DB.QueryRow(ctx,`SELECT * FROM photo.reserve_authored_render_continuation($1,$2,$3,$4,$5::jsonb,$6)`,scope.Tenant,scope.Run,r.ordinal,r.body,r.witnesses,r.bucket).Scan(&id,&token,&ready)
 if errors.Is(err,pgx.ErrNoRows){return artifactjson.Reservation{},artifactjson.ErrStale};if err!=nil{return artifactjson.Reservation{},err}
 if id==uuid.Nil||token==uuid.Nil{return artifactjson.Reservation{},objectstore.ErrIntegrity}
 out:=artifactjson.Reservation{ArtifactID:id,Token:token};if ready{out.Ready=&artifactjson.Ref{ID:id,SHA256:sha}};return out,nil
}
func(r authoredContinuationDocuments)Complete(ctx context.Context,scope artifactjson.Scope,res artifactjson.Reservation,obj objectstore.Object)(artifactjson.Ref,error){
 target,err:=artifactjson.DocumentKey(scope,res.ArtifactID)
 if err!=nil||scope!=r.scope||res.Token==uuid.Nil||obj.Key!=target||obj.SHA256!=r.sha||obj.Bytes!=int64(len(r.body))||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"{return artifactjson.Ref{},objectstore.ErrIntegrity}
 var accepted bool
 err=r.DB.QueryRow(ctx,`SELECT photo.complete_authored_render_continuation($1,$2,$3,$4,$5,$6,$7,$8,$9)`,scope.Tenant,scope.Run,r.ordinal,res.ArtifactID,res.Token,obj.SHA256,obj.Bytes,r.bucket,obj.VersionID).Scan(&accepted)
 if err!=nil{return artifactjson.Ref{},err};if !accepted{return artifactjson.Ref{},artifactjson.ErrStale};return artifactjson.Ref{ID:res.ArtifactID,SHA256:r.sha},nil
}
var _ artifactjson.Repository=authoredContinuationDocuments{}
