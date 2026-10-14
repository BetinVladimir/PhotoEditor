package provenance

import (
 "context"
 "crypto/x509"
 "encoding/json"
 "encoding/pem"
 "testing"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestSigningAPIDeploymentClosedPinnedDocument(t *testing.T){
 for _,mode:=range []string{"valid","pin","duplicate_tenant","worker","private_key","noncanonical","usage_any","origin","policy_chain","report_pin","crl_missing","negative_empty","cancelled"}{t.Run(mode,func(t *testing.T){
  certificate,_,_,serverPin:=operatorMTLSFixture(t)
  chain:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:certificate.Certificate[0]})
  roots:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:certificate.Certificate[1]})
  config,_,now:=signingRegistryFixture(t)
  var policy SigningPolicy
  if err:=json.Unmarshal(config.Policies[0].Body,&policy);err!=nil{t.Fatal(err)}
  policy.CertificateChainSHA=bodyDigest(chain);policy.TrustBundleSHA=bodyDigest(roots)
  policy.TimestampMode="off";policy.TimestampServiceID=nil;policy.RevocationMode="off"
  if mode=="policy_chain"{policy.CertificateChainSHA=policy.SDKLockSHA}
  if mode=="crl_missing"{policy.RevocationMode="required"}
  body,err:=canonical.Marshal(policy);if err!=nil{t.Fatal(err)}
  report,err:=canonical.Marshal(SigningResourceMeasurements{Schema:"signing-resource-measurements/v1",SDK:policy.SDKLockSHA,Runtime:serverPin,Contracts:serverPin,Hardware:serverPin,NotBefore:now.Add(-time.Hour).Format(time.RFC3339Nano),NotAfter:now.Add(time.Hour).Format(time.RFC3339Nano),Measurements:[]SigningResourceMeasurement{{WorkloadSHA:serverPin,RAM:64<<20,Scratch:1<<30,Output:(512<<20)+(16<<20),InputJSON:16<<20}}});if err!=nil{t.Fatal(err)}
  entry:=SigningAPITenantDocument{TenantID:uuid.New(),WorkerID:uuid.New(),QualificationSHA:serverPin,CertificateChain:string(chain),TrustBundle:string(roots),CertificateUsages:[]int{int(x509.ExtKeyUsageCodeSigning)},ChallengeOrigin:"https://signer.example",ChallengeServerSHA:serverPin,ChallengeRoots:string(roots),IdentityID:"api-identity",TimeoutSeconds:10,CRLEndpoints:[]SigningAPICRLDocument{},Policies:[]SigningAPIPolicyDocument{{Body:body,NotBefore:now.Add(-time.Hour).Format(time.RFC3339Nano),NotAfter:now.Add(time.Hour).Format(time.RFC3339Nano)}},ResourceReport:report,ResourceReportSHA:bodyDigest(report),NegativePolicies:json.RawMessage("null")}
  if mode=="worker"{entry.WorkerID=uuid.Nil}
  if mode=="usage_any"{entry.CertificateUsages=[]int{0}}
  if mode=="origin"{entry.ChallengeOrigin="https://user:password@signer.example"}
  if mode=="report_pin"{entry.ResourceReportSHA=policy.SDKLockSHA}
  if mode=="negative_empty"{entry.NegativePolicies=json.RawMessage(`{"policies":[],"schema":"signing-negative-policies/v1"}`)}
  doc:=SigningAPIDeploymentDocument{Schema:"signing-api-deployment/v1",Tenants:[]SigningAPITenantDocument{entry}}
  if mode=="duplicate_tenant"{doc.Tenants=append(doc.Tenants,entry)}
  raw,err:=canonical.Marshal(doc);if err!=nil{t.Fatal(err)}
  if mode=="private_key"{var value map[string]any;if json.Unmarshal(raw,&value)!=nil{t.Fatal("fixture")};value["private_key"]="forbidden";raw,err=canonical.Marshal(value);if err!=nil{t.Fatal(err)}}
  if mode=="noncanonical"{raw=append(raw,' ')}
  expected:=bodyDigest(raw);if mode=="pin"{expected=policy.SDKLockSHA}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  decoded,err:=DecodeSigningAPIDeploymentDocument(ctx,raw,expected)
  if (err==nil)!=(mode=="valid"){t.Fatalf("mode=%s err=%v",mode,err)}
  if mode=="valid"{raw[0]='!';if decoded.Tenants[0].TenantID!=entry.TenantID||string(decoded.Tenants[0].Policies[0].Body)!=string(body){t.Fatal("decoded ownership")}}
 })}
}
