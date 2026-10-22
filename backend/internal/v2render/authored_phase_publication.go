package v2render

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
)

// AuthoredPhasePublication publishes the exact already committed SQL receipt.
// It does not accept replacement body bytes or an active adoption lease after
// commit. Actual immutable object publication and byte/version verification are
// performed by the existing Publisher, with durable quota-backed reservation.
type AuthoredPhasePublication struct{DB artifactjson.RowQuerier;Objects artifactjson.Objects;Bucket string}
func(p AuthoredPhasePublication)Publish(ctx context.Context,tenant,parent uuid.UUID,ordinal int)(artifactjson.Ref,error){
 if p.DB==nil||p.Objects==nil||tenant==uuid.Nil||parent==uuid.Nil||ordinal<1||ordinal>=5000{return artifactjson.Ref{},objectstore.ErrInvalid}
 var stored *string
 if err:=p.DB.QueryRow(ctx,`SELECT photo.read_authored_render_phase_publication($1,$2,$3)`,tenant,parent,ordinal).Scan(&stored);err!=nil{return artifactjson.Ref{},err}
 if stored==nil{return artifactjson.Ref{},artifactjson.ErrStale}
 raw:=[]byte(*stored);wire,err:=canonical.Bytes(raw);if err!=nil||string(wire)!=*stored{return artifactjson.Ref{},objectstore.ErrIntegrity}
 var body AuthoredPhaseReceipt
 if json.Unmarshal(raw,&body)!=nil||body.Tenant!=tenant||body.Parent!=parent||body.Ordinal!=ordinal||body.RunID==uuid.Nil||body.RunID==parent{return artifactjson.Ref{},objectstore.ErrIntegrity}
 sum:=sha256.Sum256(raw);digest:=hex.EncodeToString(sum[:])
 scope:=artifactjson.Scope{Tenant:tenant,Run:body.RunID,Owner:parent,Purpose:"v26_authored_phase_receipt"}
 repo:=authoredPhaseDocuments{DB:p.DB,scope:scope,bucket:p.Bucket,sha:digest,size:int64(len(raw)),ordinal:ordinal}
 ref,err:=(artifactjson.Publisher{Repository:repo,Objects:p.Objects}).Publish(ctx,scope,raw)
 if err!=nil{return artifactjson.Ref{},err}
 // Current authority is repeated after physical IO even on the ready path.
 var after *string
 if err=p.DB.QueryRow(ctx,`SELECT photo.read_authored_render_phase_publication($1,$2,$3)`,tenant,parent,ordinal).Scan(&after);err!=nil{return artifactjson.Ref{},err}
 if after==nil||*after!=*stored{return artifactjson.Ref{},artifactjson.ErrStale}
 if err=ctx.Err();err!=nil{return artifactjson.Ref{},err};return ref,nil
}

type authoredPhaseDocuments struct {DB artifactjson.RowQuerier;scope artifactjson.Scope;bucket,sha string;size int64;ordinal int}
func(r authoredPhaseDocuments)Reserve(ctx context.Context,s artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 if r.DB==nil||s!=r.scope||sha!=r.sha||size!=r.size {return artifactjson.Reservation{},objectstore.ErrInvalid}
 var id,token uuid.UUID;var ready bool
 err:=r.DB.QueryRow(ctx,`SELECT * FROM photo.reserve_authored_render_phase_document($1,$2,$3,$4)`,s.Tenant,s.Owner,r.ordinal,r.bucket).Scan(&id,&token,&ready)
 if errors.Is(err,pgx.ErrNoRows){return artifactjson.Reservation{},artifactjson.ErrStale};if err!=nil{return artifactjson.Reservation{},err}
 if id==uuid.Nil||token==uuid.Nil{return artifactjson.Reservation{},objectstore.ErrIntegrity}
 result:=artifactjson.Reservation{ArtifactID:id,Token:token};if ready{result.Ready=&artifactjson.Ref{ID:id,SHA256:sha}};return result,nil
}
func(r authoredPhaseDocuments)Complete(ctx context.Context,s artifactjson.Scope,res artifactjson.Reservation,obj objectstore.Object)(artifactjson.Ref,error){
 key,err:=artifactjson.DocumentKey(s,res.ArtifactID)
 if err!=nil||s!=r.scope||res.Token==uuid.Nil||obj.Key!=key||obj.SHA256!=r.sha||obj.Bytes!=r.size||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"{return artifactjson.Ref{},objectstore.ErrIntegrity}
 var complete bool
 err=r.DB.QueryRow(ctx,`SELECT photo.complete_authored_render_phase_document($1,$2,$3,$4,$5,$6,$7,$8,$9)`,s.Tenant,s.Owner,r.ordinal,res.ArtifactID,res.Token,obj.SHA256,obj.Bytes,r.bucket,obj.VersionID).Scan(&complete)
 if err!=nil{return artifactjson.Ref{},err};if !complete{return artifactjson.Ref{},artifactjson.ErrStale};return artifactjson.Ref{ID:res.ArtifactID,SHA256:r.sha},nil
}
var _ artifactjson.Repository=authoredPhaseDocuments{}
