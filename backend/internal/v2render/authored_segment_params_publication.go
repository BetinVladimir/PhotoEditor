package v2render
import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
// AuthoredSegmentParamsPublication compiles from the fresh locked prefix and
// already published current-layer/protection refs. SQL repeats all provenance.
type AuthoredSegmentParamsPublication struct { Scheduler *pgxpool.Pool; Objects artifactjson.Objects; Bucket string }
func(p AuthoredSegmentParamsPublication)Publish(ctx context.Context,tenant,parent uuid.UUID,ordinal int,currentLayers,protected *v2input.Ref)(v2input.Ref,CompiledAuthoredSegmentParams,error){
 fail:=func(err error)(v2input.Ref,CompiledAuthoredSegmentParams,error){return v2input.Ref{},CompiledAuthoredSegmentParams{},err}
 if p.Scheduler==nil||p.Objects==nil||p.Bucket==""{return fail(v2input.ErrInvalid)}
 before,present,err:=ReadAuthoredSegmentInventory(ctx,p.Scheduler,tenant,parent,ordinal)
 if err!=nil{return fail(err)};if !present{return fail(artifactjson.ErrStale)}
 compiled,err:=MaterializeAuthoredSegmentParams(before,currentLayers,protected);if err!=nil{return fail(err)}
 digest:=sha256.Sum256(compiled.JSON);sha:=hex.EncodeToString(digest[:])
 scope:=artifactjson.Scope{Tenant:tenant,Run:parent,Owner:compiled.Params.InstructionID,Purpose:"task_params"}
 repository:=authoredSegmentParamsDocuments{DB:p.Scheduler,scope:scope,ordinal:ordinal,bucket:p.Bucket,body:string(compiled.JSON),sha:sha}
 published,err:=(artifactjson.Publisher{Repository:repository,Objects:p.Objects}).Publish(ctx,scope,compiled.JSON);if err!=nil{return fail(err)}
 after,present,err:=ReadAuthoredSegmentInventory(ctx,p.Scheduler,tenant,parent,ordinal)
 if err!=nil{return fail(err)};if !present{return fail(artifactjson.ErrStale)}
 a,err:=canonical.Marshal(before);if err!=nil{return fail(err)};b,err:=canonical.Marshal(after)
 if err!=nil{return fail(err)};if !bytes.Equal(a,b){return fail(artifactjson.ErrStale)}
 if err=ctx.Err();err!=nil{return fail(err)}
 return v2input.Ref{ArtifactID:published.ID,SHA256:published.SHA256},compiled,nil
}
type authoredSegmentParamsDocuments struct {
 DB artifactjson.RowQuerier
 scope artifactjson.Scope
 ordinal int
 bucket,body,sha string
}
func(r authoredSegmentParamsDocuments)Reserve(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 if r.DB==nil||scope!=r.scope||sha!=r.sha||size!=int64(len(r.body)){return artifactjson.Reservation{},objectstore.ErrInvalid}
 var id,token uuid.UUID;var ready bool
 err:=r.DB.QueryRow(ctx,`SELECT * FROM photo.reserve_authored_render_segment_params($1,$2,$3,$4,$5)`,scope.Tenant,scope.Run,r.ordinal,r.body,r.bucket).Scan(&id,&token,&ready)
 if errors.Is(err,pgx.ErrNoRows){return artifactjson.Reservation{},artifactjson.ErrStale};if err!=nil{return artifactjson.Reservation{},err}
 if id==uuid.Nil||token==uuid.Nil{return artifactjson.Reservation{},objectstore.ErrIntegrity}
 out:=artifactjson.Reservation{ArtifactID:id,Token:token};if ready{out.Ready=&artifactjson.Ref{ID:id,SHA256:sha}};return out,nil
}
func(r authoredSegmentParamsDocuments)Complete(ctx context.Context,scope artifactjson.Scope,res artifactjson.Reservation,obj objectstore.Object)(artifactjson.Ref,error){
 target,err:=artifactjson.DocumentKey(scope,res.ArtifactID)
 if err!=nil||scope!=r.scope||res.Token==uuid.Nil||obj.Key!=target||obj.SHA256!=r.sha||obj.Bytes!=int64(len(r.body))||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"{return artifactjson.Ref{},objectstore.ErrIntegrity}
 var accepted bool
 err=r.DB.QueryRow(ctx,`SELECT photo.complete_authored_render_segment_params($1,$2,$3,$4,$5,$6,$7,$8,$9)`,scope.Tenant,scope.Run,r.ordinal,res.ArtifactID,res.Token,obj.SHA256,obj.Bytes,r.bucket,obj.VersionID).Scan(&accepted)
 if err!=nil{return artifactjson.Ref{},err};if !accepted{return artifactjson.Ref{},artifactjson.ErrStale};return artifactjson.Ref{ID:res.ArtifactID,SHA256:r.sha},nil
}
var _ artifactjson.Repository=authoredSegmentParamsDocuments{}
