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

 "github.com/google/uuid"
)

func TestCertificateSigningTrustAuthority(t *testing.T){
 for _,mode:=range []string{"valid","pin","expired","native","revocation","timestamp","mutation","cancelled","eku"}{t.Run(mode,func(t *testing.T){
  now:=time.Date(2026,10,11,12,0,0,0,time.UTC)
  key,err:=ecdsa.GenerateKey(elliptic.P256(),rand.Reader);if err!=nil{t.Fatal(err)}
  ca:=&x509.Certificate{SerialNumber:big.NewInt(1),Subject:pkix.Name{CommonName:"unit CA"},NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),IsCA:true,BasicConstraintsValid:true,KeyUsage:x509.KeyUsageCertSign}
  der,err:=x509.CreateCertificate(rand.Reader,ca,ca,&key.PublicKey,key);if err!=nil{t.Fatal(err)}
  bundle:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der})
  leaf:=&x509.Certificate{SerialNumber:big.NewInt(2),NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),BasicConstraintsValid:true,KeyUsage:x509.KeyUsageDigitalSignature,ExtKeyUsage:[]x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}}
  if mode=="expired"{leaf.NotAfter=now}
  if mode=="eku"{leaf.ExtKeyUsage=[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
  der,err=x509.CreateCertificate(rand.Reader,leaf,ca,&key.PublicKey,key);if err!=nil{t.Fatal(err)}
  chain:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der})
  pin:=func(raw []byte)string{sum:=sha256.Sum256(raw);return hex.EncodeToString(sum[:])}
  policy:=SigningPolicy{Schema:1,ID:uuid.New(),Revision:"1",SDKLockSHA:pin([]byte("sdk")),KeyHandle:"unit",CertificateChainSHA:pin(chain),TrustBundleSHA:pin(bundle),AllowedMIME:[]string{"image/png"},TimestampMode:"off",RevocationMode:"off"}
  calls:=0
  native:=SigningTrustCheck(func(ctx context.Context,p SigningPolicy,c []byte)error{
   calls++
   if mode=="native"{return ErrInvalid}
   if mode=="mutation"{p.AllowedMIME[0]="changed";c[0]=0}
   return nil
  })
  if mode=="pin"{policy.TrustBundleSHA=pin([]byte("other"))}
  if mode=="revocation"{policy.RevocationMode="required"}
  if mode=="timestamp"{policy.TimestampMode="required";service:="tsa";policy.TimestampServiceID=&service}
  authority,err:=NewCertificateSigningTrustAuthority(bundle,[]x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},native,nil,nil,func()time.Time{return now});if err!=nil{t.Fatal(err)}
  // Constructor owns bundle bytes, independent of caller mutation.
  bundle[0]=0
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  if mode=="cancelled"{cancel()}
  err=authority.CheckSigningTrust(ctx,policy,chain)
  success:=mode=="valid"||mode=="mutation"
  if (err==nil)!=success{t.Fatalf("success=%v err=%v",success,err)}
  if success && (calls!=1 || policy.AllowedMIME[0]!="image/png" || chain[0]!='-'){t.Fatal("callback ownership or count")}
 })}
}

func TestCertificateTrustInstallationRejectsUnboundedUsage(t *testing.T){
 for _,usages:=range [][]x509.ExtKeyUsage{nil,{x509.ExtKeyUsageAny},{x509.ExtKeyUsageCodeSigning,x509.ExtKeyUsageCodeSigning}}{
  if _,err:=NewCertificateSigningTrustAuthority([]byte("invalid"),usages,func(context.Context,SigningPolicy,[]byte)error{return nil},nil,nil,time.Now);err==nil{t.Fatal("accepted invalid installation")}
 }
}
