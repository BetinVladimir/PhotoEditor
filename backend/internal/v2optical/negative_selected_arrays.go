package v2optical

import (
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type negativeSelectedArrayCatalog interface {resolve(context.Context,auth.Identity,VerifiedNegativeSelection,NegativeSourceArraySpec)(objectstore.Object,error)}
type SQLNegativeSelectedArrayCatalog struct {DB NegativeTransactions;Bucket string}
func selectedNegativeArraySpecs(v VerifiedNegativeSelection)([]NegativeSourceArraySpec,error){
 s:=v.source
 if s.SchemaVersion!=1||s.Route!="base"||s.Source==nil||v.raw=="" {return nil,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage([]byte(v.raw));if err!=nil||image.DType!="float32" {return nil,v2input.ErrInvalid}
 var chain struct {Steps []struct {Kind string `json:"kind"`;Inverse *v2input.Ref `json:"inverse_field"`}}
 if json.Unmarshal(image.TransformChain,&chain)!=nil||len(chain.Steps)>128 {return nil,v2input.ErrInvalid}
 for _,step:=range chain.Steps {if (step.Kind=="dense_warp")!=(step.Inverse!=nil) {return nil,v2input.ErrInvalid}}
 return sourceImageArraySpecs(image)
}
func(c SQLNegativeSelectedArrayCatalog)resolve(ctx context.Context,who auth.Identity,v VerifiedNegativeSelection,spec NegativeSourceArraySpec)(objectstore.Object,error){
 var zero objectstore.Object
 if err:=ctx.Err();err!=nil {return zero,err}
 if c.DB==nil||c.Bucket==""||who.UserID==uuid.Nil||who.SessionID==uuid.Nil {return zero,v2input.ErrInvalid}
 specs,err:=selectedNegativeArraySpecs(v);if err!=nil {return zero,err};found:=false
 for _,known:=range specs {if known==spec {found=true}};if !found {return zero,v2input.ErrInvalid}
 s:=v.source
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=c.DB.Begin(ctx);if err!=nil {return zero,err};if tx==nil {return zero,v2input.ErrInvalid};defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil {return zero,err};if role!="photo_scheduler" {return zero,v2input.ErrInvalid}
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",s.Tenant.String());err!=nil {return zero,err}
 var raw []byte
 if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_selected_array($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)",s.Tenant,s.Project,who.UserID,who.SessionID,s.Review,s.Task,s.Run,s.Hash,v.raw,spec.Ref.ArtifactID,spec.Ref.SHA256,spec.Role).Scan(&raw);err!=nil {return zero,err}
 object,err:=decodeNegativeSourceArrayObject(raw,c.Bucket,spec);if err!=nil {return zero,err}
 if err:=tx.Commit(ctx);err!=nil {return zero,err};return object,nil
}

// VerifiedNegativeSelectedArrays owns verified original array occurrences and
// version pins. SQL admission must repeat these exact pins under retained locks.
type VerifiedNegativeSelectedArrays struct {selection VerifiedNegativeSelection;specs []NegativeSourceArraySpec;objects []objectstore.Object;who auth.Identity}
func LoadNegativeSelectedArrays(ctx context.Context,db NegativeTransactions,objects v2input.SessionAnalysisObjectReader,bucket string,who auth.Identity,v VerifiedNegativeSelection)(VerifiedNegativeSelectedArrays,error){
 return loadNegativeSelectedArrays(ctx,SQLNegativeSelectedArrayCatalog{DB:db,Bucket:bucket},objects,who,v)
}
func loadNegativeSelectedArrays(ctx context.Context,catalog negativeSelectedArrayCatalog,reader v2input.SessionAnalysisObjectReader,who auth.Identity,v VerifiedNegativeSelection)(VerifiedNegativeSelectedArrays,error){
 var zero VerifiedNegativeSelectedArrays
 if err:=ctx.Err();err!=nil {return zero,err}
 if catalog==nil||reader==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil {return zero,v2input.ErrInvalid}
 specs,err:=selectedNegativeArraySpecs(v);if err!=nil {return zero,err}
 // Own private pointer-backed evidence before any callback.
 owned:=*v.source.Source;v.source.Source=&owned
 resolve:=func(spec NegativeSourceArraySpec)(objectstore.Object,error){copyValue:=v;copyLocator:=*v.source.Source;copyValue.source.Source=&copyLocator;return catalog.resolve(ctx,who,copyValue,spec)}
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
 return VerifiedNegativeSelectedArrays{selection:v,specs:append([]NegativeSourceArraySpec(nil),specs...),objects:append([]objectstore.Object(nil),pins...),who:who},nil
}

// CompileTopology requires physically verified source arrays, preserving the
// same private selection identity. It does not grant task publication authority.
func(v VerifiedNegativeSelectedArrays)CompileTopology(ctx context.Context,tenant,project,reviewID uuid.UUID,decisionHash string,plan NegativeInvalidationPlan,inventory []NegativeDependencyTask,maxTasks,maxEdges int)(NegativeReplacementTopology,error){
 if v.who.UserID==uuid.Nil||v.who.SessionID==uuid.Nil||len(v.specs)==0||len(v.specs)!=len(v.objects) {return NegativeReplacementTopology{},v2input.ErrInvalid}
 return v.selection.CompileTopology(ctx,tenant,project,reviewID,decisionHash,plan,inventory,maxTasks,maxEdges)
}
