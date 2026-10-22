package v2render

import (
 "context"
 "encoding/hex"
 "encoding/json"
 "errors"
 "crypto/sha256"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// AuthoredStyleBindingPublication publishes compiler bytes, never a style.resolve
// result. The protected SQL body scope independently authenticates the original
// occurrence, exact current adopted predecessor, runtime and all five immutable
// witness documents at BOTH reservation and physical completion.
// Effective math is repeated by the actual worker and native adopter; SQL does
// not treat a caller's effective digest as a resolve receipt.
type AuthoredStyleBindingPublication struct {
 Scheduler *pgxpool.Pool
 Objects artifactjson.Objects
 Bucket string
}

func(p AuthoredStyleBindingPublication)Publish(ctx context.Context,tenant,parent uuid.UUID,ordinal int,body []byte,witnesses map[string]string)(v2input.Ref,error){
 if p.Scheduler==nil||p.Objects==nil||p.Bucket==""||tenant==uuid.Nil||parent==uuid.Nil||ordinal<1||ordinal>4998||len(body)<2||len(body)>1048576||len(witnesses)!=5{return v2input.Ref{},v2input.ErrInvalid}
 expected:=[]string{"authored_image_jcs","authored_params_jcs","current_image_jcs","original_image_jcs","resolved_jcs"}
 for _,key:=range expected{if raw,ok:=witnesses[key];!ok||len(raw)<2||!json.Valid([]byte(raw)){return v2input.Ref{},v2input.ErrInvalid}}
 var document struct{Instruction uuid.UUID `json:"instruction_id"`}
 if err:=json.Unmarshal(body,&document);err!=nil||document.Instruction==uuid.Nil{return v2input.Ref{},v2input.ErrInvalid}
 // Rebuilding these bytes is not an authority check: the SQL scope performs
 // the strict full-document validation before any storage is reserved.
 witnessRaw,err:=canonical.Marshal(witnesses);if err!=nil{return v2input.Ref{},err}
 digest:=sha256.Sum256(body);sha:=hex.EncodeToString(digest[:])
 scope:=artifactjson.Scope{Tenant:tenant,Run:parent,Owner:document.Instruction,Purpose:"v26_authored_style_binding"}
 repository:=authoredStyleBindingDocuments{DB:p.Scheduler,scope:scope,ordinal:ordinal,bucket:p.Bucket,body:string(body),sha:sha,witnesses:string(witnessRaw)}
 ref,err:=(artifactjson.Publisher{Repository:repository,Objects:p.Objects}).Publish(ctx,scope,body)
 if err!=nil{return v2input.Ref{},err};if err=ctx.Err();err!=nil{return v2input.Ref{},err}
 return v2input.Ref{ArtifactID:ref.ID,SHA256:ref.SHA256},nil
}

type authoredStyleBindingDocuments struct {
 DB artifactjson.RowQuerier
 scope artifactjson.Scope
 ordinal int
 bucket,body,sha,witnesses string
}
func(r authoredStyleBindingDocuments)Reserve(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 if r.DB==nil||scope!=r.scope||sha!=r.sha||size!=int64(len(r.body))||!json.Valid([]byte(r.witnesses)){return artifactjson.Reservation{},objectstore.ErrInvalid}
 var id,token uuid.UUID;var ready bool
 err:=r.DB.QueryRow(ctx,`SELECT * FROM photo.reserve_authored_render_style_binding($1,$2,$3,$4,$5::jsonb,$6)`,scope.Tenant,scope.Run,r.ordinal,r.body,r.witnesses,r.bucket).Scan(&id,&token,&ready)
 if errors.Is(err,pgx.ErrNoRows){return artifactjson.Reservation{},artifactjson.ErrStale};if err!=nil{return artifactjson.Reservation{},err}
 if id==uuid.Nil||token==uuid.Nil{return artifactjson.Reservation{},objectstore.ErrIntegrity}
 out:=artifactjson.Reservation{ArtifactID:id,Token:token};if ready{out.Ready=&artifactjson.Ref{ID:id,SHA256:sha}};return out,nil
}
func(r authoredStyleBindingDocuments)Complete(ctx context.Context,scope artifactjson.Scope,res artifactjson.Reservation,obj objectstore.Object)(artifactjson.Ref,error){
 target,err:=artifactjson.DocumentKey(scope,res.ArtifactID)
 if err!=nil||scope!=r.scope||res.Token==uuid.Nil||obj.Key!=target||obj.SHA256!=r.sha||obj.Bytes!=int64(len(r.body))||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"{return artifactjson.Ref{},objectstore.ErrIntegrity}
 var accepted bool
 err=r.DB.QueryRow(ctx,`SELECT photo.complete_authored_render_style_binding($1,$2,$3,$4,$5,$6,$7,$8,$9)`,scope.Tenant,scope.Run,r.ordinal,res.ArtifactID,res.Token,obj.SHA256,obj.Bytes,r.bucket,obj.VersionID).Scan(&accepted)
 if err!=nil{return artifactjson.Ref{},err};if !accepted{return artifactjson.Ref{},artifactjson.ErrStale};return artifactjson.Ref{ID:res.ArtifactID,SHA256:r.sha},nil
}
var _ artifactjson.Repository=authoredStyleBindingDocuments{}
