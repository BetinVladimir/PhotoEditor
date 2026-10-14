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

type adoptedParentArrayCatalog interface {resolve(context.Context,auth.Identity,VerifiedAdoptedOpticalParent,NegativeSourceArraySpec)(objectstore.Object,error)}
type SQLAdoptedParentArrayCatalog struct {DB NegativeTransactions;Bucket string}
func adoptedParentArraySpecs(v VerifiedAdoptedOpticalParent)([]NegativeSourceArraySpec,error){
 s:=v.metadata
 packet,err:=canonical.Marshal(s);if err!=nil {return nil,err}
 if _,err:=decodeAdoptedOpticalParent(packet,s.Source.Bucket,s.Tenant,s.Project,s.Task,s.Run);err!=nil {return nil,err}
 sum:=sha256.Sum256([]byte(v.raw))
 if s.Tenant==uuid.Nil||s.Project==uuid.Nil||s.Task==uuid.Nil||s.Run==uuid.Nil||int64(len(v.raw))!=s.Source.Bytes||hex.EncodeToString(sum[:])!=s.Source.SHA256 {return nil,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage([]byte(v.raw));if err!=nil||image.DType!="float32" {return nil,v2input.ErrInvalid}
 var chain struct {Steps []struct {Kind string `json:"kind"`;Inverse *v2input.Ref `json:"inverse_field"`}}
 if json.Unmarshal(image.TransformChain,&chain)!=nil||len(chain.Steps)>128 {return nil,v2input.ErrInvalid}
 for _,step:=range chain.Steps {if (step.Kind=="dense_warp")!=(step.Inverse!=nil) {return nil,v2input.ErrInvalid}}
 return sourceImageArraySpecs(image)
}
func(c SQLAdoptedParentArrayCatalog)resolve(ctx context.Context,who auth.Identity,v VerifiedAdoptedOpticalParent,spec NegativeSourceArraySpec)(objectstore.Object,error){
 var zero objectstore.Object
 if err:=ctx.Err();err!=nil {return zero,err}
 if c.DB==nil||c.Bucket==""||who.UserID==uuid.Nil||who.SessionID==uuid.Nil {return zero,v2input.ErrInvalid}
 specs,err:=adoptedParentArraySpecs(v);if err!=nil {return zero,err};found:=false
 for _,known:=range specs {if known==spec {found=true}};if !found {return zero,v2input.ErrInvalid}
 s:=v.metadata
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=c.DB.Begin(ctx);if err!=nil {return zero,err};if tx==nil {return zero,v2input.ErrInvalid};defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil {return zero,err};if role!="photo_scheduler" {return zero,v2input.ErrInvalid}
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",s.Tenant.String());err!=nil {return zero,err}
 var raw []byte
 if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_adopted_optical_parent_array($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)",s.Tenant,s.Project,who.UserID,who.SessionID,s.Task,s.Run,s.Result,v.raw,spec.Ref.ArtifactID,spec.Ref.SHA256,spec.Role).Scan(&raw);err!=nil {return zero,err}
 object,err:=decodeNegativeSourceArrayObject(raw,c.Bucket,spec);if err!=nil {return zero,err}
 if err:=tx.Commit(ctx);err!=nil {return zero,err};return object,nil
}

// VerifiedAdoptedParentArrays owns original numeric array proof and version pins.
// Replacement membership and current SQL admission locks remain mandatory.
type VerifiedAdoptedParentArrays struct {selection VerifiedAdoptedOpticalParent;specs []NegativeSourceArraySpec;objects []objectstore.Object;who auth.Identity}
func LoadAdoptedParentArrays(ctx context.Context,db NegativeTransactions,objects v2input.SessionAnalysisObjectReader,bucket string,who auth.Identity,v VerifiedAdoptedOpticalParent)(VerifiedAdoptedParentArrays,error){
 return loadAdoptedParentArrays(ctx,SQLAdoptedParentArrayCatalog{DB:db,Bucket:bucket},objects,who,v)
}
func loadAdoptedParentArrays(ctx context.Context,catalog adoptedParentArrayCatalog,reader v2input.SessionAnalysisObjectReader,who auth.Identity,v VerifiedAdoptedOpticalParent)(VerifiedAdoptedParentArrays,error){
 var zero VerifiedAdoptedParentArrays
 if err:=ctx.Err();err!=nil {return zero,err}
 if catalog==nil||reader==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil {return zero,v2input.ErrInvalid}
 specs,err:=adoptedParentArraySpecs(v);if err!=nil {return zero,err}
 if v.who.UserID!=who.UserID||v.who.SessionID!=who.SessionID {return zero,v2input.ErrInvalid}
 resolve:=func(spec NegativeSourceArraySpec)(objectstore.Object,error){return catalog.resolve(ctx,who,v,spec)}
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
 return VerifiedAdoptedParentArrays{selection:v,specs:append([]NegativeSourceArraySpec(nil),specs...),objects:append([]objectstore.Object(nil),pins...),who:who},nil
}

