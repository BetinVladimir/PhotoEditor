package provenance

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

func TestClosedInstalledOperatorNeverInvokesDependencies(t *testing.T){
 var nilAuthority *InstalledChallengeSigningAuthority
 if nilAuthority.CheckSigningOperator(context.Background(),SigningPolicy{})==nil{t.Fatal("nil authority accepted")}
 nilAuthority.Close()
 lifetime,cancel:=context.WithCancel(context.Background())
 authority:=&InstalledChallengeSigningAuthority{lifetime:lifetime,cancel:cancel}
 authority.Close();authority.Close()
 if !errors.Is(lifetime.Err(),context.Canceled){t.Fatal("lifetime was not cancelled")}
 if authority.CheckSigningOperator(context.Background(),SigningPolicy{})==nil{t.Fatal("closed authority accepted")}
 ctx,stop:=context.WithCancel(context.Background());stop()
 if !errors.Is(authority.CheckSigningOperator(ctx,SigningPolicy{}),context.Canceled){t.Fatal("caller cancellation lost")}
}

func TestOperatorInstallationRequiresIndependentNativeAuthority(t *testing.T){
 if _,err:=NewInstalledChallengeSigningAuthority(SigningOperatorInstallation{});err==nil{t.Fatal("missing authority accepted")}
}

func TestInstalledSigningServiceRefusesCompetingAuthority(t *testing.T){
 config:=PresentationSigningServiceInstallation{Authority:operatorChallengeFuncAuthority{}}
 if _,err:=NewInstalledSQLSigningService(config,SigningOperatorInstallation{});err==nil{t.Fatal("competing authority accepted")}
}
type operatorChallengeFuncAuthority struct{}
func(operatorChallengeFuncAuthority)CheckSigningOperator(context.Context,SigningPolicy)error{return nil}

func TestInstalledSigningServiceRefusesForeignTenantBeforeDownstreamIO(t *testing.T){
 tenant:=uuid.New()
 service:=&InstalledSQLSigningService{SQLSignExportService:&SQLSignExportService{},TenantID:tenant}
 for _,foreign:=range []uuid.UUID{uuid.Nil,uuid.New()}{
  if _,err:=service.SignExport(context.Background(),auth.Identity{},foreign,uuid.New(),uuid.New(),"key");err==nil{t.Fatal("foreign tenant signed")}
  if _,err:=service.AvailableSigningPolicies(context.Background(),auth.Identity{},foreign,uuid.New());err==nil{t.Fatal("foreign tenant policies exposed")}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=service.SignExport(ctx,auth.Identity{},tenant,uuid.New(),uuid.New(),"key");err!=context.Canceled{t.Fatal("cancellation lost")}
 if _,err:=service.AvailableSigningPolicies(ctx,auth.Identity{},tenant,uuid.New());err!=context.Canceled{t.Fatal("policy cancellation lost")}
}
