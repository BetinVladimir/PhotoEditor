package provenance

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

func TestNegativeProducerFallbackOnlyOnMissingExistingOutput(t *testing.T){
 denied:=errors.New("authority denied")
 for _,cause:=range []error{nil,denied,pgx.ErrNoRows}{
  calls:=0;producer:=uuid.New()
  existing:=signingProducerFunc(func(context.Context,auth.Identity,ReadySigningExport,v2input.Ref)(uuid.UUID,error){if cause!=nil{return uuid.Nil,cause};return producer,nil})
  negative:=signingProducerFunc(func(context.Context,auth.Identity,ReadySigningExport,v2input.Ref)(uuid.UUID,error){calls++;return producer,nil})
  id,err:=(SigningImageProducerWithNegative{Existing:existing,Negative:negative}).ResolveSigningImageProducer(context.Background(),auth.Identity{},ReadySigningExport{},v2input.Ref{})
  if cause==pgx.ErrNoRows{if calls!=1||err!=nil||id!=producer{t.Fatal("missing producer did not resolve negative child",err)}}else if calls!=0||!errors.Is(err,cause){t.Fatal("fallback hid existing authority outcome",err)}
 }
}

func TestNegativeTaskRouterNeverRetriesRejectedNegative(t *testing.T){
 denied:=errors.New("negative evidence denied")
 for _,operation:=range []string{"optical.negative","render.compose","encode","unknown"}{
  existingCalls,negativeCalls:=0,0
  existing:=signingTaskClaimsFunc(func(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask)(ResolvedSigningTask,error){existingCalls++;return ResolvedSigningTask{},nil})
  negative:=signingTaskClaimsFunc(func(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask)(ResolvedSigningTask,error){negativeCalls++;return ResolvedSigningTask{},denied})
  _,err:=(NegativeSigningTaskRouter{Existing:existing,Negative:negative}).ResolveSigningTaskLineage(context.Background(),auth.Identity{},ReadySigningExport{},CanonicalSigningTask{Operation:operation})
  if operation=="optical.negative"{if negativeCalls!=1||existingCalls!=0||!errors.Is(err,denied){t.Fatal("negative denial bypassed",err)}}else if existingCalls!=1||negativeCalls!=0||err!=nil{t.Fatal("existing route changed",operation,err)}
 }
}

func TestNegativeInstallationRejectsMissingDependencies(t *testing.T){
 if _,err:=NewSQLSigningNegativeEvidence(nil,"photos",nil,nil,nil);err==nil{t.Fatal("unconfigured evidence installed")}
 if _,err:=NewSQLNegativeRenderPresentationSigningExecution(nil,"photos",nil,"",nil);err==nil{t.Fatal("unconfigured execution installed")}
 if _,err:=NewSQLNegativeRenderPresentationSigningExportSource(nil,"photos",nil,nil,"",nil);err==nil{t.Fatal("missing lineage publisher installed")}
 if _,err:=NewSQLNegativeRenderPresentationSigningService(PresentationSigningServiceInstallation{},nil);err==nil{t.Fatal("missing policies installed")}
}
