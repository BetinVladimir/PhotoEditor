package v2optical

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "photoeditor/backend/internal/canonical"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type negativeUnchangedArrayCatalog interface {resolve(context.Context,auth.Identity,VerifiedNegativeUnchangedSource,NegativeSourceArraySpec)(objectstore.Object,error)}
type SQLNegativeUnchangedArrayCatalog struct {DB NegativeTransactions;Bucket string}
func unchangedSourceArraySpecs(v VerifiedNegativeUnchangedSource)([]NegativeSourceArraySpec,error){
 packet,err:=canonical.Marshal(v.locator);if err!=nil{return nil,err}
 if _,err:=decodeNegativeUnchangedSource(packet,v.locator.Bucket,v.binding.Source);err!=nil{return nil,err}
 originals,err:=opticalFrozenBindings(v.step.Operation(),[]byte(v.step.intent.raw));if err!=nil{return nil,err}
 count:=0;for _,binding:=range originals{if binding==v.binding{count++}};if count!=1{return nil,v2input.ErrInvalid}
 sum:=sha256.Sum256([]byte(v.raw))
 if int64(len(v.raw))!=v.locator.Bytes||hex.EncodeToString(sum[:])!=v.locator.SHA256{return nil,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage([]byte(v.raw));if err!=nil||image.DType!="float32" {return nil,v2input.ErrInvalid}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil||canvas!=v.binding.CanvasHash{return nil,v2input.ErrInvalid}
 var chain struct {Steps []struct {Kind string `json:"kind"`;Inverse *v2input.Ref `json:"inverse_field"`}}
 if json.Unmarshal(image.TransformChain,&chain)!=nil||len(chain.Steps)>128 {return nil,v2input.ErrInvalid}
 for _,step:=range chain.Steps {if (step.Kind=="dense_warp")!=(step.Inverse!=nil) {return nil,v2input.ErrInvalid}}
 return sourceImageArraySpecs(image)
}
func(c SQLNegativeUnchangedArrayCatalog)resolve(ctx context.Context,who auth.Identity,v VerifiedNegativeUnchangedSource,spec NegativeSourceArraySpec)(objectstore.Object,error){
 var zero objectstore.Object
 if err:=ctx.Err();err!=nil {return zero,err}
 if c.DB==nil||c.Bucket==""||who.UserID==uuid.Nil||who.SessionID==uuid.Nil {return zero,v2input.ErrInvalid}
 specs,err:=unchangedSourceArraySpecs(v);if err!=nil {return zero,err};found:=false
 for _,known:=range specs {if known==spec {found=true}};if !found {return zero,v2input.ErrInvalid}
 s:=v.step.intent.locator
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=c.DB.Begin(ctx);if err!=nil {return zero,err};if tx==nil {return zero,v2input.ErrInvalid};defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil {return zero,err};if role!="photo_scheduler" {return zero,v2input.ErrInvalid}
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",s.Tenant.String());err!=nil {return zero,err}
 var raw []byte
 if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_unchanged_array($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)",s.Tenant,s.Project,who.UserID,who.SessionID,s.Review,s.Hash,s.Task,v.binding.Source.ArtifactID,v.binding.Source.SHA256,v.raw,spec.Ref.ArtifactID,spec.Ref.SHA256,spec.Role).Scan(&raw);err!=nil {return zero,err}
 object,err:=decodeNegativeSourceArrayObject(raw,c.Bucket,spec);if err!=nil {return zero,err}
 if err:=tx.Commit(ctx);err!=nil {return zero,err};return object,nil
}

// VerifiedNegativeUnchangedArrays owns unchanged original numeric array proof and version pins.
// Replacement membership and current SQL admission locks remain mandatory.
type VerifiedNegativeUnchangedArrays struct {selection VerifiedNegativeUnchangedSource;specs []NegativeSourceArraySpec;objects []objectstore.Object;who auth.Identity}
func LoadNegativeUnchangedArrays(ctx context.Context,db NegativeTransactions,objects v2input.SessionAnalysisObjectReader,bucket string,who auth.Identity,v VerifiedNegativeUnchangedSource)(VerifiedNegativeUnchangedArrays,error){
 return loadNegativeUnchangedArrays(ctx,SQLNegativeUnchangedArrayCatalog{DB:db,Bucket:bucket},objects,who,v)
}
func loadNegativeUnchangedArrays(ctx context.Context,catalog negativeUnchangedArrayCatalog,reader v2input.SessionAnalysisObjectReader,who auth.Identity,v VerifiedNegativeUnchangedSource)(VerifiedNegativeUnchangedArrays,error){
 var zero VerifiedNegativeUnchangedArrays
 if err:=ctx.Err();err!=nil {return zero,err}
 if catalog==nil||reader==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil {return zero,v2input.ErrInvalid}
 specs,err:=unchangedSourceArraySpecs(v);if err!=nil {return zero,err}
 if v.step.intent.who.UserID!=who.UserID||v.step.intent.who.SessionID!=who.SessionID {return zero,v2input.ErrInvalid}
 v.step.node.Parents=append([]NegativeReplacementParent(nil),v.step.node.Parents...)
 resolve:=func(spec NegativeSourceArraySpec)(objectstore.Object,error){owned:=v;owned.step.node.Parents=append([]NegativeReplacementParent(nil),v.step.node.Parents...);return catalog.resolve(ctx,who,owned,spec)}
 pins:=make([]objectstore.Object,len(specs))
 for i,spec:=range specs {
  object,err:=resolve(spec);if err!=nil {return zero,err}
  stream,err:=reader.Open(ctx,object);if err!=nil {if stream!=nil {_=stream.Close()};return zero,err};if stream==nil {return zero,v2input.ErrInvalid}
  verifyErr:=VerifyNegativeSourceArray(ctx,spec,object,stream);closeErr:=stream.Close()
  if verifyErr!=nil {return zero,verifyErr};if closeErr!=nil {return zero,closeErr}
  current,err:=resolve(spec);if err!=nil {return zero,err};if current!=object {return zero,v2input.ErrExpired}
  pins[i]=object
 }
 for i,spec:=range specs {current,err:=resolve(spec);if err!=nil {return zero,err};if current!=pins[i] {return zero,v2input.ErrExpired}}
 if err:=ctx.Err();err!=nil {return zero,err}
 return VerifiedNegativeUnchangedArrays{selection:v,specs:append([]NegativeSourceArraySpec(nil),specs...),objects:append([]objectstore.Object(nil),pins...),who:who},nil
}

