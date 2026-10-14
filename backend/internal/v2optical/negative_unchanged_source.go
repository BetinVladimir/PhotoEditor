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
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type VerifiedNegativeUnchangedSource struct{step PreparedNegativeReplacementStep;binding v2input.SessionVersionBinding;locator negativeSourceLocator;raw string}
func(v VerifiedNegativeUnchangedSource)WorkingImageBytes()[]byte{return []byte(v.raw)}
func decodeNegativeUnchangedSource(raw []byte,bucket string,ref v2input.Ref)(negativeSourceLocator,error){
 var out negativeSourceLocator
 if len(raw)<2||len(raw)>16384{return out,v2input.ErrExpired}
 normal,err:=canonical.Bytes(raw);if err!=nil{return out,v2input.ErrInvalid}
 if json.Unmarshal(normal,&out)!=nil{return out,v2input.ErrInvalid};exact,err:=canonical.Marshal(out)
 if err!=nil||!bytes.Equal(exact,normal)||out.ArtifactID!=ref.ArtifactID||out.SHA256!=ref.SHA256||!validPerspectiveRef(ref)||bucket==""||out.Bucket!=bucket||out.Key==""||len(out.Key)>1024||out.Version==""||out.Version=="null"||len(out.Version)>1024||out.Bytes<2||out.Bytes>65536||out.Media!="application/json"{return negativeSourceLocator{},v2input.ErrInvalid}
 return out,nil
}
func LoadNegativeUnchangedSource(ctx context.Context,db NegativeTransactions,objects v2input.SessionAnalysisObjectReader,bucket string,step PreparedNegativeReplacementStep,binding v2input.SessionVersionBinding)(VerifiedNegativeUnchangedSource,error){
 var zero VerifiedNegativeUnchangedSource
 if err:=ctx.Err();err!=nil{return zero,err};if db==nil||objects==nil{return zero,v2input.ErrInvalid}
 originals,err:=opticalFrozenBindings(step.Operation(),[]byte(step.intent.raw));if err!=nil{return zero,err}
 count:=0;for _,original:=range originals{if original==binding{count++}};if count!=1{return zero,v2input.ErrInvalid}
 l:=step.intent.locator;who:=step.intent.who
 if who.UserID==uuid.Nil||who.SessionID==uuid.Nil||l.Tenant==uuid.Nil||l.Project==uuid.Nil{return zero,v2input.ErrInvalid}
 // Validate complete original intent before using any scoped lookup.
 packet,err:=canonical.Marshal(l);if err!=nil{return zero,err}
 if _,err=decodeNegativeFrozenIntent(packet,bucket,l.Tenant,l.Project,l.Review,l.Task,l.Hash);err!=nil{return zero,err}
 sum:=sha256.Sum256([]byte(step.intent.raw));if hex.EncodeToString(sum[:])!=l.Params.SHA256||int64(len(step.intent.raw))!=l.Params.Bytes||step.node.OriginalTask!=l.Task||step.node.OriginalRun!=l.Run{return zero,v2input.ErrInvalid}
 lookup:=func()(negativeSourceLocator,error){
  queryCtx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
  tx,err:=db.Begin(queryCtx);if err!=nil{return negativeSourceLocator{},err};if tx==nil{return negativeSourceLocator{},v2input.ErrInvalid};defer tx.Rollback(context.WithoutCancel(queryCtx))
  var role string;if err=tx.QueryRow(queryCtx,"SELECT current_user").Scan(&role);err!=nil{return negativeSourceLocator{},err};if role!="photo_scheduler"{return negativeSourceLocator{},v2input.ErrInvalid}
  if _,err=tx.Exec(queryCtx,"SELECT set_config('app.tenant_id',$1,true)",l.Tenant.String());err!=nil{return negativeSourceLocator{},err}
  var raw []byte
  if err=tx.QueryRow(queryCtx,"SELECT photo.read_v29_negative_unchanged_source($1,$2,$3,$4,$5,$6,$7,$8,$9)",l.Tenant,l.Project,who.UserID,who.SessionID,l.Review,l.Hash,l.Task,binding.Source.ArtifactID,binding.Source.SHA256).Scan(&raw);err!=nil{return negativeSourceLocator{},err}
  out,err:=decodeNegativeUnchangedSource(raw,bucket,binding.Source);if err!=nil{return negativeSourceLocator{},err}
  if err=tx.Commit(queryCtx);err!=nil{return negativeSourceLocator{},err};return out,nil
 }
 before,err:=lookup();if err!=nil{return zero,err}
 reader,err:=objects.Open(ctx,objectstore.Object{Key:before.Key,VersionID:before.Version,SHA256:before.SHA256,Bytes:before.Bytes,ContentType:before.Media})
 if err!=nil{if reader!=nil{_=reader.Close()};return zero,err};if reader==nil{return zero,v2input.ErrInvalid}
 raw,readErr:=io.ReadAll(io.LimitReader(reader,before.Bytes+1));closeErr:=reader.Close()
 if readErr!=nil{return zero,readErr};if closeErr!=nil{return zero,closeErr};if err=ctx.Err();err!=nil{return zero,err}
 digest:=sha256.Sum256(raw);if int64(len(raw))!=before.Bytes||hex.EncodeToString(digest[:])!=before.SHA256{return zero,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(raw);if err!=nil||image.DType!="float32"{return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(image);if err!=nil||!bytes.Equal(exact,raw){return zero,v2input.ErrInvalid}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil||canvas!=binding.CanvasHash{return zero,v2input.ErrInvalid}
 after,err:=lookup();if err!=nil{return zero,err};if after!=before{return zero,v2input.ErrExpired}
 step.node.Parents=append([]NegativeReplacementParent(nil),step.node.Parents...)
 return VerifiedNegativeUnchangedSource{step:step,binding:binding,locator:before,raw:string(raw)},nil
}

