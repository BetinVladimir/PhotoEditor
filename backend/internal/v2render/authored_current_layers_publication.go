package v2render

import (
 "context"
 "encoding/hex"
 "encoding/json"
 "errors"
 "crypto/sha256"
 "bytes"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// AuthoredCurrentLayersPublication retains current Params separately from the
// segment task Params. Both use the same original occurrence UUID but distinct
// closed purposes; task_params must never be reused for this intermediate doc.
// SQL independently authenticates original settings, actual style binding and
// adopted mask-transform evidence at reservation and physical completion.
type AuthoredCurrentLayersPublication struct {
 Scheduler *pgxpool.Pool
 Objects artifactjson.Objects
 Bucket string
}

func(p AuthoredCurrentLayersPublication)Publish(ctx context.Context,tenant,parent uuid.UUID,ordinal int,body []byte,bindings json.RawMessage)(v2input.Ref,error){
 if p.Scheduler==nil||p.Objects==nil||p.Bucket==""||len(body)<2||len(body)>1048576||len(bindings)<2||len(bindings)>8388608{return v2input.Ref{},v2input.ErrInvalid}
 var proofs []json.RawMessage
 if json.Unmarshal(bindings,&proofs)!=nil||proofs==nil||len(proofs)>256{return v2input.Ref{},v2input.ErrInvalid}
 before,found,err:=ReadAuthoredSegmentInventory(ctx,p.Scheduler,tenant,parent,ordinal)
 if err!=nil{return v2input.Ref{},err};if !found{return v2input.Ref{},artifactjson.ErrStale}
 witnesses,err:=canonical.Marshal(proofs);if err!=nil{return v2input.Ref{},err}
 digest:=sha256.Sum256(body);sha:=hex.EncodeToString(digest[:])
 scope:=artifactjson.Scope{Tenant:tenant,Run:parent,Owner:before.Context.Instruction.Identity,Purpose:"v26_authored_current_layers"}
 repository:=authoredCurrentLayersDocuments{DB:p.Scheduler,scope:scope,ordinal:ordinal,bucket:p.Bucket,body:string(body),sha:sha,witnesses:string(witnesses)}
 ref,err:=(artifactjson.Publisher{Repository:repository,Objects:p.Objects}).Publish(ctx,scope,body)
 if err!=nil{return v2input.Ref{},err}
 after,found,err:=ReadAuthoredSegmentInventory(ctx,p.Scheduler,tenant,parent,ordinal)
 if err!=nil{return v2input.Ref{},err};if !found{return v2input.Ref{},artifactjson.ErrStale}
 beforeRaw,err:=canonical.Marshal(before);if err!=nil{return v2input.Ref{},err};afterRaw,err:=canonical.Marshal(after)
 if err!=nil{return v2input.Ref{},err};if !bytes.Equal(beforeRaw,afterRaw){return v2input.Ref{},artifactjson.ErrStale}
 if err=ctx.Err();err!=nil{return v2input.Ref{},err};return v2input.Ref{ArtifactID:ref.ID,SHA256:ref.SHA256},nil
}

type authoredCurrentLayersDocuments struct {
 DB artifactjson.RowQuerier
 scope artifactjson.Scope
 ordinal int
 bucket,body,sha,witnesses string
}
func(r authoredCurrentLayersDocuments)Reserve(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 if r.DB==nil||scope!=r.scope||sha!=r.sha||size!=int64(len(r.body))||!json.Valid([]byte(r.witnesses)){return artifactjson.Reservation{},objectstore.ErrInvalid}
 var id,token uuid.UUID;var ready bool
 err:=r.DB.QueryRow(ctx,`SELECT * FROM photo.reserve_authored_render_current_layers($1,$2,$3,$4,$5::jsonb,$6)`,scope.Tenant,scope.Run,r.ordinal,r.body,r.witnesses,r.bucket).Scan(&id,&token,&ready)
 if errors.Is(err,pgx.ErrNoRows){return artifactjson.Reservation{},artifactjson.ErrStale};if err!=nil{return artifactjson.Reservation{},err}
 if id==uuid.Nil||token==uuid.Nil{return artifactjson.Reservation{},objectstore.ErrIntegrity}
 out:=artifactjson.Reservation{ArtifactID:id,Token:token};if ready{out.Ready=&artifactjson.Ref{ID:id,SHA256:sha}};return out,nil
}
func(r authoredCurrentLayersDocuments)Complete(ctx context.Context,scope artifactjson.Scope,res artifactjson.Reservation,obj objectstore.Object)(artifactjson.Ref,error){
 target,err:=artifactjson.DocumentKey(scope,res.ArtifactID)
 if err!=nil||scope!=r.scope||res.Token==uuid.Nil||obj.Key!=target||obj.SHA256!=r.sha||obj.Bytes!=int64(len(r.body))||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"{return artifactjson.Ref{},objectstore.ErrIntegrity}
 var accepted bool
 err=r.DB.QueryRow(ctx,`SELECT photo.complete_authored_render_current_layers($1,$2,$3,$4,$5,$6,$7,$8,$9)`,scope.Tenant,scope.Run,r.ordinal,res.ArtifactID,res.Token,obj.SHA256,obj.Bytes,r.bucket,obj.VersionID).Scan(&accepted)
 if err!=nil{return artifactjson.Ref{},err};if !accepted{return artifactjson.Ref{},artifactjson.ErrStale};return artifactjson.Ref{ID:res.ArtifactID,SHA256:r.sha},nil
}
var _ artifactjson.Repository=authoredCurrentLayersDocuments{}
