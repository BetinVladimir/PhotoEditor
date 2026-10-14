package provenance

import (
 "context"
 "testing"
 "github.com/google/uuid"
)

func TestRegistrationTrustRefusesMissingDependenciesAndCancellation(t *testing.T){
 for _,config:=range []SQLSigningRegistrationTrust{
  {},{TenantID:uuid.New(),WorkerID:uuid.New(),QualificationSHA:"invalid"},
  {TenantID:uuid.New(),WorkerID:uuid.New(),QualificationSHA:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",LiveTrust:func(context.Context,SigningPolicy,[]byte)error{t.Fatal("missing DB reached live trust");return nil}},
 }{
  if config.Check(context.Background(),SigningPolicy{},nil)==nil{t.Fatal("missing dependencies accepted")}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if (SQLSigningRegistrationTrust{}).CheckInstallation(ctx)!=context.Canceled{t.Fatal("startup cancellation lost")}
 if (SQLSigningRegistrationTrust{}).CheckInstallation(context.Background())==nil{t.Fatal("startup missing dependencies accepted")}
 if (SQLSigningRegistrationTrust{}).Check(ctx,SigningPolicy{},nil)!=context.Canceled{t.Fatal("cancellation lost")}
}
