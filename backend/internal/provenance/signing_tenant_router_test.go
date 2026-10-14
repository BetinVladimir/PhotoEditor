package provenance

import (
 "context"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

func tenantSigningFixture() *InstalledSQLSigningService{
 tenant:=uuid.New()
 return &InstalledSQLSigningService{TenantID:tenant,SQLSignExportService:&SQLSignExportService{Preparation:SigningActivationPreparation{Registry:&SigningPolicyRegistry{tenant:tenant}}},Operator:&InstalledChallengeSigningAuthority{}}
}
func TestTenantSigningRouterOwnsCatalogueAndLifecycle(t *testing.T){
 a,b:=tenantSigningFixture(),tenantSigningFixture()
 tenantA,tenantB,operatorA,operatorB:=a.TenantID,b.TenantID,a.Operator,b.Operator
 inputs:=[]*InstalledSQLSigningService{a,b}
 router,err:=NewTenantSigningRouter(inputs);if err!=nil{t.Fatal(err)}
 defer router.Close()
 a.TenantID=tenantB;a.Operator=operatorB;a.SQLSignExportService=nil;inputs[0]=nil
 selected,err:=router.service(context.Background(),tenantA)
 if err!=nil||selected.TenantID!=tenantA||selected.Operator!=operatorA||selected.SQLSignExportService==nil{t.Fatal("caller mutated owned route",err)}
 selected,err=router.service(context.Background(),tenantB)
 if err!=nil||selected.Operator!=operatorB{t.Fatal("cross-tenant operator",err)}
 if _,err=router.SignExport(context.Background(),auth.Identity{},uuid.New(),uuid.New(),uuid.New(),"key");err==nil{t.Fatal("fallback signing accepted")}
 if _,err=router.AvailableSigningPolicies(context.Background(),auth.Identity{},uuid.New(),uuid.New());err==nil{t.Fatal("fallback policies exposed")}
 router.Close();router.Close()
 if !operatorA.closed.Load()||!operatorB.closed.Load(){t.Fatal("operator lifecycle not closed")}
 if _,err=router.service(context.Background(),tenantA);err==nil{t.Fatal("closed route accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=router.service(ctx,tenantA);err!=context.Canceled{t.Fatal("cancellation lost")}
}
func TestTenantSigningRouterRefusesConflictingInstallationsWithoutClosing(t *testing.T){
 a,b:=tenantSigningFixture(),tenantSigningFixture()
 duplicate:=*b;duplicate.TenantID=a.TenantID
 shared:=*b;shared.Operator=a.Operator
 for _,inputs:=range [][]*InstalledSQLSigningService{nil,{nil},{a,&duplicate},{a,&shared}}{
  if _,err:=NewTenantSigningRouter(inputs);err==nil{t.Fatal("conflicting installation accepted")}
  if a.Operator.closed.Load()||b.Operator.closed.Load(){t.Fatal("failed construction stole lifecycle")}
 }
 a.Close()
 if _,err:=NewTenantSigningRouter([]*InstalledSQLSigningService{a});err==nil{t.Fatal("closed installation accepted")}
}
