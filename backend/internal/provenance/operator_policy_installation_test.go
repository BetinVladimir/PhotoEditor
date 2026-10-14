package provenance

import (
 "context"
 "crypto/ecdsa"
 "crypto/elliptic"
 "crypto/rand"
 "crypto/sha256"
 "crypto/x509"
 "crypto/x509/pkix"
 "encoding/hex"
 "encoding/pem"
 "math/big"
 "testing"
 "time"
)

func TestOperatorPolicyCoverageBeforeServiceConstruction(t *testing.T){
 for _,mode:=range []string{"valid","chain","trust","timestamp","revocation","wrong_issuer","duplicate"}{t.Run(mode,func(t *testing.T){
  key,err:=ecdsa.GenerateKey(elliptic.P256(),rand.Reader);if err!=nil{t.Fatal(err)}
  now:=time.Date(2026,10,11,12,0,0,0,time.UTC)
  ca:=&x509.Certificate{SerialNumber:big.NewInt(1),Subject:pkix.Name{CommonName:"coverage CA"},NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),IsCA:true,BasicConstraintsValid:true,KeyUsage:x509.KeyUsageCertSign|x509.KeyUsageCRLSign}
  caDER,err:=x509.CreateCertificate(rand.Reader,ca,ca,&key.PublicKey,key);if err!=nil{t.Fatal(err)}
  leaf:=&x509.Certificate{SerialNumber:big.NewInt(2),NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),KeyUsage:x509.KeyUsageDigitalSignature}
  der,err:=x509.CreateCertificate(rand.Reader,leaf,ca,&key.PublicKey,key);if err!=nil{t.Fatal(err)}
  chain:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der})
  trust:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:caDER})
  pin:=func(raw []byte)string{sum:=sha256.Sum256(raw);return hex.EncodeToString(sum[:])}
  policies,_,_:=signingRegistryFixture(t)
  var policy SigningPolicy
  if err=decode(policies.Policies[0].Body,&policy,1<<20);err!=nil{t.Fatal(err)}
  policy.CertificateChainSHA=pin(chain);policy.TrustBundleSHA=pin(trust)
  raw,ref:=contractBytes(t,policy)
  policies.CertificateChainSHA=policy.CertificateChainSHA;policies.TrustBundleSHA=policy.TrustBundleSHA
  policies.Policies[0].Body=raw;policies.Policies[0].Ref=ref
  operator:=SigningOperatorInstallation{CertificateChain:chain,TrustBundle:trust,
   Timestamp:func(context.Context,SigningPolicy,[]byte)error{t.Fatal("structural validation invoked TSA");return nil},
   CRLEndpoints:[]SigningCRLEndpoint{{IssuerSHA:pin(caDER)}}}
  switch mode{
  case "chain":operator.CertificateChain=trust
  case "trust":operator.TrustBundle=chain
  case "timestamp":operator.Timestamp=nil
  case "revocation":operator.CRLEndpoints=nil
  case "wrong_issuer":operator.CRLEndpoints[0].IssuerSHA=pin(der)
  case "duplicate":operator.CRLEndpoints=append(operator.CRLEndpoints,operator.CRLEndpoints[0])
  }
  authority:=&signingAuthorityStub{}
  err=validateSigningOperatorPolicies(policies,operator,authority)
  if (err==nil)!=(mode=="valid"){t.Fatalf("mode=%s err=%v",mode,err)}
  if authority.calls!=0{t.Fatal("structural validation called live operator")}
 })}
}
