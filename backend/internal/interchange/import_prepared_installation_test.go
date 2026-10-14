package interchange

import (
 "context"
 "errors"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "testing"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type preparedInstallationMaskQualifier func(context.Context,review.MaskRef,v2input.SessionVersionBinding)error
func(f preparedInstallationMaskQualifier)QualifyImportedMask(ctx context.Context,m review.MaskRef,b v2input.SessionVersionBinding)error{return f(ctx,m,b)}

func TestPreparedImportInstallationRequiresAuthorities(t *testing.T){
 if _,err:=NewSQLPreparedImportResolver(context.Background(),PreparedImportResolverDependencies{});!errors.Is(err,v2input.ErrInvalid){t.Fatalf("missing authority: %v",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=NewSQLPreparedImportResolver(ctx,PreparedImportResolverDependencies{});!errors.Is(err,context.Canceled){t.Fatalf("cancel: %v",err)}
}
func TestPreparedMaskQualificationRequiresBothStages(t *testing.T){
 called:=false
 stage:=preparedInstallationMaskQualifier(func(context.Context,review.MaskRef,v2input.SessionVersionBinding)error{called=true;return nil})
 for _,q:=range []importPreparedMaskQualification{{Physical:stage},{Semantic:stage},{}}{
  if err:=q.QualifyImportedMask(context.Background(),review.MaskRef{},v2input.SessionVersionBinding{});!errors.Is(err,v2input.ErrInvalid){t.Fatalf("missing stage: %v",err)}
 }
 if called{t.Fatal("incomplete chain invoked authority")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=(importPreparedMaskQualification{Physical:stage,Semantic:stage}).QualifyImportedMask(ctx,review.MaskRef{},v2input.SessionVersionBinding{});!errors.Is(err,context.Canceled){t.Fatalf("cancel: %v",err)}
 if called{t.Fatal("cancelled chain invoked authority")}
}

// The semantic callback may perform IO or retain/mutate received buffers. The
// final physical pass must see the original mask and current storage authority.
func TestPreparedMaskQualificationSequenceAndIsolation(t *testing.T){
 for _,failure:=range []string{"none","physical","semantic","recheck","cancel"}{t.Run(failure,func(t *testing.T){
  _,_,imageRaw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  raw,err:=canonical.Marshal(map[string]any{"artifact":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))},"canvas":image.Canvas,"transform_chain":json.RawMessage(image.TransformChain),"selector":"manual","confidence":1,"protected":true});if err!=nil{t.Fatal(err)}
  mask,err:=review.ParseMaskRef(raw);if err!=nil{t.Fatal(err)}
  raw,err=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
  sentinel:=errors.New("authority changed")
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  calls:=[]string{};physicalCalls:=0
  physical:=preparedInstallationMaskQualifier(func(_ context.Context,m review.MaskRef,_ v2input.SessionVersionBinding)error{
   calls=append(calls,"physical");physicalCalls++
   got,e:=canonical.Marshal(m);if e!=nil||string(got)!=string(raw){t.Fatal("mask mutated across stages")}
   if failure=="physical"||(failure=="recheck"&&physicalCalls==2){return sentinel};return nil
  })
  semantic:=preparedInstallationMaskQualifier(func(_ context.Context,m review.MaskRef,_ v2input.SessionVersionBinding)error{
   calls=append(calls,"semantic")
   if len(m.TransformChain)>0{m.TransformChain[0]='!'}
   if failure=="semantic"{return sentinel};if failure=="cancel"{cancel()};return nil
  })
  err=(importPreparedMaskQualification{Physical:physical,Semantic:semantic}).QualifyImportedMask(ctx,mask,v2input.SessionVersionBinding{})
  expected:=3
  switch failure{case "physical":expected=1;case "semantic","cancel":expected=2}
  if len(calls)!=expected{t.Fatalf("stages %v",calls)}
  if failure=="none"{if err!=nil{t.Fatal(err)}}else if failure=="cancel"{if !errors.Is(err,context.Canceled){t.Fatal(err)}}else if !errors.Is(err,sentinel){t.Fatal(err)}
  if calls[0]!="physical"||(len(calls)>1&&calls[1]!="semantic"){t.Fatalf("order %v",calls)}
 })}
}
