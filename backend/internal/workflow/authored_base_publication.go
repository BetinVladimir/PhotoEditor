package workflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
)

// AuthoredBasePublication performs the existing durable reserve -> immutable
// object PUT/recovery -> version-verified completion -> locked phase append.
// Callers must construct body from the actual full Apply producer inventory
// and independently read/validate every typed image/quality/coverage witness.
// SQL checks that body against all canonical task/result/profile tuples again.
// It never calls ordinary collection adoption or creates an identity seal.
type AuthoredBasePublication struct { DB artifactjson.RowQuerier; Objects artifactjson.Objects; Bucket string }
func (p AuthoredBasePublication) Complete(ctx context.Context,l ApplyLease,parent uuid.UUID,raw []byte)(artifactjson.Ref,error){
 if p.DB==nil||p.Objects==nil||parent==uuid.Nil||parent==l.RunID||l.Token==uuid.Nil||l.ItemCount!=1||l.Phase!="adopt" {return artifactjson.Ref{},objectstore.ErrInvalid}
 body,err:=canonical.Bytes(raw);if err!=nil||len(body)<2||len(body)>1048576||body[0]!='{' {return artifactjson.Ref{},objectstore.ErrInvalid}
 if err=ctx.Err();err!=nil{return artifactjson.Ref{},err}
 var staged bool
 err=p.DB.QueryRow(ctx,`SELECT photo.stage_authored_render_base_receipt($1,$2,$3,$4,$5)`,l.TenantID,parent,l.RunID,l.Token,string(body)).Scan(&staged)
 if err!=nil{return artifactjson.Ref{},err};if !staged{return artifactjson.Ref{},artifactjson.ErrStale}
 digest:=sha256.Sum256(body);sha:=hex.EncodeToString(digest[:])
 scope:=artifactjson.Scope{Tenant:l.TenantID,Run:l.RunID,Owner:parent,Purpose:"v26_authored_base_receipt"}
 repo:=authoredBaseDocuments{DB:p.DB,scope:scope,bucket:p.Bucket,sha:sha,size:int64(len(body))}
 ref,err:=(artifactjson.Publisher{Repository:repo,Objects:p.Objects}).Publish(ctx,scope,body)
 if err!=nil{return artifactjson.Ref{},err}
 var committed bool
 err=p.DB.QueryRow(ctx,`SELECT photo.commit_authored_render_base_phase($1,$2,$3,$4,$5,$6)`,l.TenantID,parent,l.RunID,l.Token,ref.ID,ref.SHA256).Scan(&committed)
 if err!=nil{return artifactjson.Ref{},err};if !committed{return artifactjson.Ref{},artifactjson.ErrStale};return ref,nil
}

type authoredBaseDocuments struct {DB artifactjson.RowQuerier;scope artifactjson.Scope;bucket,sha string;size int64}
func(r authoredBaseDocuments)Reserve(ctx context.Context,s artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 if r.DB==nil||s!=r.scope||sha!=r.sha||size!=r.size {return artifactjson.Reservation{},objectstore.ErrInvalid}
 var id,token uuid.UUID;var ready bool
 err:=r.DB.QueryRow(ctx,`SELECT * FROM photo.reserve_authored_render_base_document($1,$2,$3)`,s.Tenant,s.Owner,r.bucket).Scan(&id,&token,&ready)
 if errors.Is(err,pgx.ErrNoRows){return artifactjson.Reservation{},artifactjson.ErrStale};if err!=nil{return artifactjson.Reservation{},err}
 if id==uuid.Nil||token==uuid.Nil{return artifactjson.Reservation{},objectstore.ErrIntegrity}
 result:=artifactjson.Reservation{ArtifactID:id,Token:token};if ready{result.Ready=&artifactjson.Ref{ID:id,SHA256:sha}};return result,nil
}
func(r authoredBaseDocuments)Complete(ctx context.Context,s artifactjson.Scope,res artifactjson.Reservation,obj objectstore.Object)(artifactjson.Ref,error){
 key,err:=artifactjson.DocumentKey(s,res.ArtifactID)
 if err!=nil||s!=r.scope||res.Token==uuid.Nil||obj.Key!=key||obj.SHA256!=r.sha||obj.Bytes!=r.size||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"{return artifactjson.Ref{},objectstore.ErrIntegrity}
 var complete bool
 err=r.DB.QueryRow(ctx,`SELECT photo.complete_authored_render_base_document($1,$2,$3,$4,$5,$6,$7,$8)`,s.Tenant,s.Owner,res.ArtifactID,res.Token,obj.SHA256,obj.Bytes,r.bucket,obj.VersionID).Scan(&complete)
 if err!=nil{return artifactjson.Ref{},err};if !complete{return artifactjson.Ref{},artifactjson.ErrStale};return artifactjson.Ref{ID:res.ArtifactID,SHA256:r.sha},nil
}
var _ artifactjson.Repository=authoredBaseDocuments{}
