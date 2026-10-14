package provenance

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "testing"
 "github.com/google/uuid"
)

func TestAtomicTenantSigningInstallation(t *testing.T){
 for _,mode:=range []string{"valid","second_failure","cancelled","wrong_tenant","partial_failure"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  configs:=[]TenantSigningInstallation{
   {Operator:SigningOperatorInstallation{Registration:&SQLSigningRegistrationTrust{TenantID:uuid.New(),WorkerID:uuid.New()}}},
   {Operator:SigningOperatorInstallation{Registration:&SQLSigningRegistrationTrust{TenantID:uuid.New(),WorkerID:uuid.New()}}},
  }
  services:=[]*InstalledSQLSigningService{}
  calls:=0
  build:=func(ctx context.Context,_ PresentationSigningServiceInstallation,operator SigningOperatorInstallation)(*InstalledSQLSigningService,error){
   calls++
   if mode=="second_failure"&&calls==2{return nil,ErrInvalid}
   service:=tenantSigningFixture();service.TenantID=operator.Registration.TenantID;service.Preparation.Registry.tenant=service.TenantID
   services=append(services,service)
   if mode=="wrong_tenant"{service.TenantID=uuid.New()}
   if mode=="cancelled"{cancel()}
   if mode=="partial_failure"{return service,ErrInvalid}
   return service,nil
  }
  router,err:=buildInstalledTenantSigningRouter(ctx,configs,build)
  if mode=="valid"{
   if err!=nil||router==nil||calls!=2{t.Fatal("valid installation",calls,err)}
   for _,service:=range services{if service.Operator.closed.Load(){t.Fatal("success closed operator")}}
   router.Close()
  }else if err==nil||router!=nil{t.Fatal("partial router exposed",mode,err)}
  for _,service:=range services{if !service.Operator.closed.Load(){t.Fatal("failed installation leaked operator",mode)}}
 })}
}
func TestDuplicateTenantSigningDeclarationsRefuseBeforeBuild(t *testing.T){
 registration:=&SQLSigningRegistrationTrust{TenantID:uuid.New(),WorkerID:uuid.New()}
 configs:=[]TenantSigningInstallation{{Operator:SigningOperatorInstallation{Registration:registration}},{Operator:SigningOperatorInstallation{Registration:registration}}}
 if _,err:=buildInstalledTenantSigningRouter(context.Background(),configs,func(context.Context,PresentationSigningServiceInstallation,SigningOperatorInstallation)(*InstalledSQLSigningService,error){t.Fatal("duplicate declaration reached build");return nil,nil});err==nil{t.Fatal("duplicate tenant accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=NewInstalledChallengeSigningAuthorityContext(ctx,SigningOperatorInstallation{});err!=context.Canceled{t.Fatal("operator startup cancellation lost")}
 if _,err:=NewInstalledSQLSigningServiceContext(ctx,PresentationSigningServiceInstallation{},SigningOperatorInstallation{});err!=context.Canceled{t.Fatal("service startup cancellation lost")}
}

func TestTenantSigningCatalogueFreezesAllDeclarationsBeforeFirstCallback(t *testing.T){
 first,second:=uuid.New(),uuid.New()
 configs:=[]TenantSigningInstallation{
  {Operator:SigningOperatorInstallation{Registration:&SQLSigningRegistrationTrust{TenantID:first,WorkerID:uuid.New()}}},
  {Service:PresentationSigningServiceInstallation{ResourceReport:[]byte("report"),Policies:SigningInstallation{KeyHandles:[]string{"key"},TimestampServices:[]string{"tsa"},Policies:[]InstalledSigningPolicy{{Body:[]byte("policy")}}}},
   Operator:SigningOperatorInstallation{Registration:&SQLSigningRegistrationTrust{TenantID:second,WorkerID:uuid.New()},CertificateChain:[]byte("chain"),TrustBundle:[]byte("trust"),CertificateUsages:[]x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},CRLEndpoints:[]SigningCRLEndpoint{{URL:"https://original"}},ChallengeIdentity:tls.Certificate{Certificate:[][]byte{[]byte("identity")}},ChallengeRoots:x509.NewCertPool(),CRLRoots:x509.NewCertPool()}},
 }
 calls:=0
 router,err:=buildInstalledTenantSigningRouter(context.Background(),configs,func(_ context.Context,service PresentationSigningServiceInstallation,operator SigningOperatorInstallation)(*InstalledSQLSigningService,error){
  calls++
  if calls==1{
   configs[1].Service.ResourceReport[0]='!';configs[1].Service.Policies.Policies[0].Body[0]='!'
   configs[1].Service.Policies.KeyHandles[0]="changed";configs[1].Service.Policies.TimestampServices[0]="changed"
   configs[1].Operator.CertificateChain[0]='!';configs[1].Operator.TrustBundle[0]='!'
   configs[1].Operator.CertificateUsages[0]=x509.ExtKeyUsageAny
   configs[1].Operator.CRLEndpoints[0].URL="https://changed"
   configs[1].Operator.ChallengeIdentity.Certificate[0][0]='!'
   configs[1].Operator.Registration.TenantID=uuid.New()
  }else{
   if string(service.ResourceReport)!="report"||string(service.Policies.Policies[0].Body)!="policy"||service.Policies.KeyHandles[0]!="key"||service.Policies.TimestampServices[0]!="tsa"{t.Fatal("service declaration mutation")}
   if operator.Registration.TenantID!=second||string(operator.CertificateChain)!="chain"||string(operator.TrustBundle)!="trust"||operator.CertificateUsages[0]!=x509.ExtKeyUsageCodeSigning||operator.CRLEndpoints[0].URL!="https://original"||string(operator.ChallengeIdentity.Certificate[0])!="identity"{t.Fatal("operator declaration mutation")}
   if operator.ChallengeRoots==configs[1].Operator.ChallengeRoots||operator.CRLRoots==configs[1].Operator.CRLRoots{t.Fatal("root pool not owned")}
  }
  built:=tenantSigningFixture();built.TenantID=operator.Registration.TenantID;built.Preparation.Registry.tenant=built.TenantID;return built,nil
 })
 if err!=nil{t.Fatal(err)};defer router.Close()
 if calls!=2{t.Fatal("missing tenant construction")}
}
