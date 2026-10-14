package v2optical

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type adoptedOpticalParent struct{
 SchemaVersion int `json:"schema_version"`
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Task uuid.UUID `json:"task_id"`
 Run uuid.UUID `json:"run_id"`
 Result uuid.UUID `json:"result_id"`
 Attempt int64 `json:"attempt"`
 Fence int64 `json:"fencing_token"`
 Operation string `json:"operation"`
 Source negativeSourceLocator `json:"source"`
}
func decodeAdoptedOpticalParent(raw []byte,bucket string,tenant,project,task,run uuid.UUID)(adoptedOpticalParent,error){
 var out adoptedOpticalParent
 if len(raw)<2||len(raw)>32768{return out,v2input.ErrExpired}
 normalized,err:=canonical.Bytes(raw);if err!=nil{return out,v2input.ErrInvalid}
 if json.Unmarshal(normalized,&out)!=nil{return adoptedOpticalParent{},v2input.ErrInvalid}
 exact,err:=canonical.Marshal(out);s:=out.Source
 if err!=nil||!bytes.Equal(exact,normalized)||out.SchemaVersion!=1||out.Tenant!=tenant||out.Project!=project||out.Task!=task||out.Run!=run||out.Result==uuid.Nil||out.Attempt<1||out.Fence<1{return adoptedOpticalParent{},v2input.ErrInvalid}
 switch out.Operation{case "optical.negative","optical.perspective","optical.panorama","optical.focus_stack":default:return adoptedOpticalParent{},v2input.ErrInvalid}
 if !validPerspectiveRef(v2input.Ref{ArtifactID:s.ArtifactID,SHA256:s.SHA256})||s.Bucket!=bucket||s.Key==""||len(s.Key)>1024||s.Version==""||s.Version=="null"||len(s.Version)>1024||s.Bytes<2||s.Bytes>65536||s.Media!="application/json"{return adoptedOpticalParent{},v2input.ErrInvalid}
 return out,nil
}
type adoptedOpticalParentCatalog interface{lookup(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID)(adoptedOpticalParent,error)}
type sqlAdoptedOpticalParentCatalog struct{db NegativeTransactions;bucket string}
func(c sqlAdoptedOpticalParentCatalog)lookup(ctx context.Context,who auth.Identity,tenant,project,task,run uuid.UUID)(adoptedOpticalParent,error){
 var zero adoptedOpticalParent
 if err:=ctx.Err();err!=nil{return zero,err}
 if c.db==nil||c.bucket==""||tenant==uuid.Nil||project==uuid.Nil||task==uuid.Nil||run==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return zero,v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=c.db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid};defer tx.Rollback(context.WithoutCancel(ctx))
 var role string;if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return zero,err};if role!="photo_scheduler"{return zero,v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{return zero,err}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_adopted_optical_parent($1,$2,$3,$4,$5,$6)",tenant,project,who.UserID,who.SessionID,task,run).Scan(&raw);err!=nil{return zero,err}
 value,err:=decodeAdoptedOpticalParent(raw,c.bucket,tenant,project,task,run);if err!=nil{return zero,err}
 if err=tx.Commit(ctx);err!=nil{return zero,err};return value,nil
}

// VerifiedAdoptedOpticalParent proves original WorkingImage document bytes and
// repeated adoption metadata. Array closure and replacement membership are not
// implied; admission must independently retain current SQL/source proof locks.
type VerifiedAdoptedOpticalParent struct{metadata adoptedOpticalParent;raw string;who auth.Identity}
func(v VerifiedAdoptedOpticalParent)WorkingImageBytes()[]byte{return []byte(v.raw)}
func(v VerifiedAdoptedOpticalParent)SourceRef()v2input.Ref{return v2input.Ref{ArtifactID:v.metadata.Source.ArtifactID,SHA256:v.metadata.Source.SHA256}}
func LoadAdoptedOpticalParent(ctx context.Context,db NegativeTransactions,objects v2input.SessionAnalysisObjectReader,bucket string,who auth.Identity,tenant,project,task,run uuid.UUID)(VerifiedAdoptedOpticalParent,error){
 return loadAdoptedOpticalParent(ctx,sqlAdoptedOpticalParentCatalog{db,bucket},objects,who,tenant,project,task,run)
}
func loadAdoptedOpticalParent(ctx context.Context,catalog adoptedOpticalParentCatalog,objects v2input.SessionAnalysisObjectReader,who auth.Identity,tenant,project,task,run uuid.UUID)(VerifiedAdoptedOpticalParent,error){
 var zero VerifiedAdoptedOpticalParent
 if err:=ctx.Err();err!=nil{return zero,err};if catalog==nil||objects==nil{return zero,v2input.ErrInvalid}
 before,err:=catalog.lookup(ctx,who,tenant,project,task,run);if err!=nil{return zero,err}
 // Revalidate even an injected catalogue before using its object size/path.
 packet,err:=canonical.Marshal(before);if err!=nil{return zero,err}
 if tenant==uuid.Nil||project==uuid.Nil||task==uuid.Nil||run==uuid.Nil||before.Source.Bucket==""{return zero,v2input.ErrInvalid}
 before,err=decodeAdoptedOpticalParent(packet,before.Source.Bucket,tenant,project,task,run);if err!=nil{return zero,err}
 s:=before.Source
 reader,err:=objects.Open(ctx,objectstore.Object{Key:s.Key,VersionID:s.Version,SHA256:s.SHA256,Bytes:s.Bytes,ContentType:s.Media})
 if err!=nil{if reader!=nil{_=reader.Close()};return zero,err};if reader==nil{return zero,v2input.ErrInvalid}
 raw,readErr:=io.ReadAll(io.LimitReader(reader,s.Bytes+1));closeErr:=reader.Close()
 if readErr!=nil{return zero,readErr};if closeErr!=nil{return zero,closeErr};if err=ctx.Err();err!=nil{return zero,err}
 sum:=sha256.Sum256(raw);if int64(len(raw))!=s.Bytes||hex.EncodeToString(sum[:])!=s.SHA256{return zero,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(raw);if err!=nil||image.DType!="float32"{return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(image);if err!=nil||!bytes.Equal(exact,raw){return zero,v2input.ErrInvalid}
 after,err:=catalog.lookup(ctx,who,tenant,project,task,run);if err!=nil{return zero,err}
 a,err:=canonical.Marshal(after);if err!=nil||!bytes.Equal(packet,a){return zero,v2input.ErrExpired}
 return VerifiedAdoptedOpticalParent{metadata:before,raw:string(raw),who:who},nil
}
