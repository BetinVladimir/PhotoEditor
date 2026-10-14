package provenance

import (
 "context"
 "crypto/x509"
 "github.com/google/uuid"
)

type TenantSigningInstallation struct{
 Service PresentationSigningServiceInstallation
 Operator SigningOperatorInstallation
}

func NewInstalledTenantSigningRouter(ctx context.Context,configs []TenantSigningInstallation)(*TenantSigningRouter,error){
 return buildInstalledTenantSigningRouter(ctx,configs,NewInstalledSQLSigningServiceContext)
}

func buildInstalledTenantSigningRouter(ctx context.Context,configs []TenantSigningInstallation,build func(context.Context,PresentationSigningServiceInstallation,SigningOperatorInstallation)(*InstalledSQLSigningService,error))(*TenantSigningRouter,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if build==nil||len(configs)<1||len(configs)>100000{return nil,ErrInvalid}
 owned,err:=freezeTenantSigningInstallations(configs);if err!=nil{return nil,err}
 seen:=map[uuid.UUID]bool{}
 // Validate identity declarations before creating any SQL/native/HTTP resource.
 for i:=range owned{
  registration:=owned[i].Operator.Registration
  if registration==nil||registration.TenantID==uuid.Nil||registration.WorkerID==uuid.Nil||seen[registration.TenantID]{return nil,ErrInvalid}
  copy:=*registration;owned[i].Operator.Registration=&copy
  seen[registration.TenantID]=true
 }
 built:=make([]*InstalledSQLSigningService,0,len(owned))
 success:=false
 defer func(){if !success{for _,service:=range built{service.Close()}}}()
 for _,config:=range owned{
  if err:=ctx.Err();err!=nil{return nil,err}
  service,err:=build(ctx,config.Service,config.Operator)
  if service!=nil{built=append(built,service)}
  if err!=nil{return nil,err}
  if service==nil||service.TenantID!=config.Operator.Registration.TenantID{return nil,ErrInvalid}
 }
 if err:=ctx.Err();err!=nil{return nil,err}
 router,err:=NewTenantSigningRouter(built);if err!=nil{return nil,err}
 if err=ctx.Err();err!=nil{router.Close();return nil,err}
 success=true
 return router,nil
}

// Bound and freeze mutable provisioning data before the first startup callback.
// Database/object/native handles remain explicit trusted interfaces; private
// signing keys are never serialized or copied into policy documents.
func freezeTenantSigningInstallations(configs []TenantSigningInstallation)([]TenantSigningInstallation,error){
 const maximum=128<<20
 used:=0
 charge:=func(size int)bool{if size>maximum-used{return false};used+=size;return true}
 for _,config:=range configs{
  if len(config.Service.NegativePolicies)>256||len(config.Operator.CRLEndpoints)>64||len(config.Service.Policies.Policies)>MaxInstalledSigningPolicies||len(config.Service.Policies.KeyHandles)>1024||len(config.Service.Policies.TimestampServices)>1024{return nil,ErrInvalid}
  for _,entry:=range config.Service.NegativePolicies{
   for _,values:=range [][]string{entry.Locks.RequiredChecks,entry.Locks.CalibrationSHAs,entry.Locks.TestFixtureSHAs,entry.Locks.EvidenceSHAs}{
    if len(values)>10000{return nil,ErrInvalid}
    for _,value:=range values{if !charge(len(value)){return nil,ErrInvalid}}
   }
  }
  for _,raw:=range [][]byte{config.Service.ResourceReport,config.Operator.CertificateChain,config.Operator.TrustBundle,config.Operator.ChallengeIdentity.OCSPStaple}{if !charge(len(raw)){return nil,ErrInvalid}}
  for _,raw:=range config.Operator.ChallengeIdentity.Certificate{if !charge(len(raw)){return nil,ErrInvalid}}
  for _,policy:=range config.Service.Policies.Policies{if !charge(len(policy.Body)){return nil,ErrInvalid}}
  for _,value:=range config.Service.Policies.KeyHandles{if !charge(len(value)){return nil,ErrInvalid}}
  for _,value:=range config.Service.Policies.TimestampServices{if !charge(len(value)){return nil,ErrInvalid}}
  for _,endpoint:=range config.Operator.CRLEndpoints{if !charge(len(endpoint.URL)+len(endpoint.IssuerSHA)+len(endpoint.ServerSHA)){return nil,ErrInvalid}}
 }
 owned:=append([]TenantSigningInstallation(nil),configs...)
 for i:=range owned{
  service,operator:=&owned[i].Service,&owned[i].Operator
  if service.NegativePolicies!=nil{
   service.NegativePolicies=append(make([]InstalledSigningNegativePolicy,0,len(service.NegativePolicies)),service.NegativePolicies...)
   for j:=range service.NegativePolicies{entry:=&service.NegativePolicies[j];entry.Locks=ownSigningNegativeLocks(entry.Locks)}
  }
  service.ResourceReport=append([]byte(nil),service.ResourceReport...)
  service.Policies.KeyHandles=append([]string(nil),service.Policies.KeyHandles...)
  service.Policies.TimestampServices=append([]string(nil),service.Policies.TimestampServices...)
  service.Policies.Policies=append([]InstalledSigningPolicy(nil),service.Policies.Policies...)
  for j:=range service.Policies.Policies{entry:=&service.Policies.Policies[j];entry.Body=append([]byte(nil),entry.Body...)}
  operator.CertificateChain=append([]byte(nil),operator.CertificateChain...)
  operator.TrustBundle=append([]byte(nil),operator.TrustBundle...)
  operator.CertificateUsages=append([]x509.ExtKeyUsage(nil),operator.CertificateUsages...)
  operator.CRLEndpoints=append([]SigningCRLEndpoint(nil),operator.CRLEndpoints...)
  identity:=operator.ChallengeIdentity
  identity.Certificate=make([][]byte,len(operator.ChallengeIdentity.Certificate))
  for j,raw:=range operator.ChallengeIdentity.Certificate{identity.Certificate[j]=append([]byte(nil),raw...)}
  identity.OCSPStaple=append([]byte(nil),identity.OCSPStaple...)
  identity.Leaf=nil;identity.SignedCertificateTimestamps=nil
  operator.ChallengeIdentity=identity
  if operator.ChallengeRoots!=nil{operator.ChallengeRoots=operator.ChallengeRoots.Clone()}
  if operator.CRLRoots!=nil{operator.CRLRoots=operator.CRLRoots.Clone()}
  if operator.Registration!=nil{copy:=*operator.Registration;operator.Registration=&copy}
 }
 return owned,nil
}
