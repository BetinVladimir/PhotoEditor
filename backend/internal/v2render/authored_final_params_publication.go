package v2render

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// AuthoredFinalParamsPublication uses the real continuation publisher and
// current transcript. It has no caller-supplied Params/ref or accepted output.
type AuthoredFinalParamsPublication struct { Continuation AuthoredContinuationPublication }
func(p AuthoredFinalParamsPublication)Publish(ctx context.Context,who auth.Identity,tenant,parent uuid.UUID,ordinal int)(v2input.Ref,CompiledAuthoredFinalParams,error){
 fail:=func(err error)(v2input.Ref,CompiledAuthoredFinalParams,error){return v2input.Ref{},CompiledAuthoredFinalParams{},err}
 continuation,compiled,err:=p.Continuation.Publish(ctx,who,tenant,parent,ordinal);if err!=nil{return fail(err)}
 documents,found,err:=ReadAuthoredFinalDocuments(ctx,p.Continuation.Scheduler,p.Continuation.PhaseObjects,who,tenant,parent,ordinal)
 if err!=nil{return fail(err)};if !found{return fail(artifactjson.ErrStale)}
 expected,err:=MaterializeAuthoredFinalParams(documents,continuation);if err!=nil||!bytes.Equal(compiled.JSON,expected.JSON){return fail(v2input.ErrInvalid)}
 digest:=sha256.Sum256(compiled.JSON);sha:=hex.EncodeToString(digest[:])
 scope:=artifactjson.Scope{Tenant:tenant,Run:parent,Owner:compiled.Params.InstructionID,Purpose:"task_params"}
 repository:=authoredFinalParamsDocuments{DB:p.Continuation.Scheduler,scope:scope,ordinal:ordinal,bucket:p.Continuation.Bucket,body:string(compiled.JSON),sha:sha}
 published,err:=(artifactjson.Publisher{Repository:repository,Objects:p.Continuation.Objects}).Publish(ctx,scope,compiled.JSON);if err!=nil{return fail(err)}
 after,present,err:=ReadAuthoredFinalInventory(ctx,p.Continuation.Scheduler,tenant,parent,ordinal);if err!=nil{return fail(err)};if !present{return fail(artifactjson.ErrStale)}
 a,err:=canonical.Marshal(documents.Inventory);if err!=nil{return fail(err)};b,err:=canonical.Marshal(after);if err!=nil||!bytes.Equal(a,b){return fail(artifactjson.ErrStale)}
 return v2input.Ref{ArtifactID:published.ID,SHA256:published.SHA256},compiled,nil
}

type authoredFinalParamsDocuments struct {
 DB artifactjson.RowQuerier
 scope artifactjson.Scope
 ordinal int
 bucket,body,sha string
}
func(r authoredFinalParamsDocuments)Reserve(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 if r.DB==nil||scope!=r.scope||sha!=r.sha||size!=int64(len(r.body)){return artifactjson.Reservation{},objectstore.ErrInvalid}
 var id,token uuid.UUID;var ready bool
 err:=r.DB.QueryRow(ctx,`SELECT * FROM photo.reserve_authored_render_final_params($1,$2,$3,$4,$5)`,scope.Tenant,scope.Run,r.ordinal,r.body,r.bucket).Scan(&id,&token,&ready)
 if errors.Is(err,pgx.ErrNoRows){return artifactjson.Reservation{},artifactjson.ErrStale};if err!=nil{return artifactjson.Reservation{},err}
 if id==uuid.Nil||token==uuid.Nil{return artifactjson.Reservation{},objectstore.ErrIntegrity}
 out:=artifactjson.Reservation{ArtifactID:id,Token:token};if ready{out.Ready=&artifactjson.Ref{ID:id,SHA256:sha}};return out,nil
}
func(r authoredFinalParamsDocuments)Complete(ctx context.Context,scope artifactjson.Scope,res artifactjson.Reservation,obj objectstore.Object)(artifactjson.Ref,error){
 target,err:=artifactjson.DocumentKey(scope,res.ArtifactID)
 if err!=nil||scope!=r.scope||res.Token==uuid.Nil||obj.Key!=target||obj.SHA256!=r.sha||obj.Bytes!=int64(len(r.body))||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"{return artifactjson.Ref{},objectstore.ErrIntegrity}
 var accepted bool
 err=r.DB.QueryRow(ctx,`SELECT photo.complete_authored_render_final_params($1,$2,$3,$4,$5,$6,$7,$8,$9)`,scope.Tenant,scope.Run,r.ordinal,res.ArtifactID,res.Token,obj.SHA256,obj.Bytes,r.bucket,obj.VersionID).Scan(&accepted)
 if err!=nil{return artifactjson.Ref{},err};if !accepted{return artifactjson.Ref{},artifactjson.ErrStale};return artifactjson.Ref{ID:res.ArtifactID,SHA256:r.sha},nil
}
var _ artifactjson.Repository=authoredFinalParamsDocuments{}
