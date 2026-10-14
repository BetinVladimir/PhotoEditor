package interchange

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type backgroundTargetSources struct{calls int;seen ImportCorrectionBinding;failure error}
func(s *backgroundTargetSources)QualifyImportRecipeSource(_ context.Context,entry uuid.UUID,old,target v2input.SessionVersionBinding)error{s.calls++;s.seen=ImportCorrectionBinding{Entry:entry,Old:old,New:target};return s.failure}
func TestBackgroundTargetAuthorityOwnsExactSchedulerBinding(t *testing.T){
 makeBinding:=func()v2input.SessionVersionBinding{return v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},CanvasHash:strings.Repeat("b",64)}}
 original:=ImportCorrectionBinding{Entry:uuid.New(),Old:makeBinding(),New:makeBinding()}
 bindings:=[]ImportCorrectionBinding{original};sources:=&backgroundTargetSources{}
 authority,err:=NewBoundImportBackgroundAuthority(bindings,sources);if err!=nil{t.Fatal(err)}
 bindings[0].New=makeBinding()
 if err:=authority.QualifyImportBackgroundTarget(context.Background(),original.New);err!=nil||sources.seen!=original{t.Fatal("lost owned mapping",err)}
 changed:=original.New;changed.CanvasHash=strings.Repeat("c",64)
 if err:=authority.QualifyImportBackgroundTarget(context.Background(),changed);err==nil||sources.calls!=1{t.Fatal("accepted changed target")}
 sentinel:=errors.New("revoked master");sources.failure=sentinel
 if err:=authority.QualifyImportBackgroundTarget(context.Background(),original.New);!errors.Is(err,sentinel){t.Fatal("lost current authority refusal",err)}
 if _,err:=NewBoundImportBackgroundAuthority([]ImportCorrectionBinding{original,original},sources);err==nil{t.Fatal("accepted duplicate target")}
 ctx,cancel:=context.WithCancel(context.Background());cancel();before:=sources.calls
 if err:=authority.QualifyImportBackgroundTarget(ctx,original.New);!errors.Is(err,context.Canceled)||sources.calls!=before{t.Fatal("ignored cancellation",err)}
}
