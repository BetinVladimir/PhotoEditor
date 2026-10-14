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

func TestSigningCRLCheck(t *testing.T){
 for _,mode:=range []string{"valid","revoked","expired","future","stale","wrong_signature","partial","source_error","cancelled","mutation","rollback","equivocation","revoked_rollback","advance"}{t.Run(mode,func(t *testing.T){
  now:=time.Date(2026,10,11,12,0,0,0,time.UTC)
  key,err:=ecdsa.GenerateKey(elliptic.P256(),rand.Reader);if err!=nil{t.Fatal(err)}
  ca:=&x509.Certificate{SerialNumber:big.NewInt(1),Subject:pkix.Name{CommonName:"unit CRL CA"},NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),IsCA:true,BasicConstraintsValid:true,KeyUsage:x509.KeyUsageCertSign|x509.KeyUsageCRLSign,SubjectKeyId:[]byte{1,2,3}}
  der,err:=x509.CreateCertificate(rand.Reader,ca,ca,&key.PublicKey,key);if err!=nil{t.Fatal(err)}
  issuer,err:=x509.ParseCertificate(der);if err!=nil{t.Fatal(err)}
  bundle:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der})
  leaf:=&x509.Certificate{SerialNumber:big.NewInt(2),NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),KeyUsage:x509.KeyUsageDigitalSignature}
  der,err=x509.CreateCertificate(rand.Reader,leaf,issuer,&key.PublicKey,key);if err!=nil{t.Fatal(err)}
  chain:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der})
  sum:=sha256.Sum256(chain);policy:=SigningPolicy{RevocationMode:"required",CertificateChainSHA:hex.EncodeToString(sum[:])}
  template:=&x509.RevocationList{Number:big.NewInt(1),ThisUpdate:now.Add(-time.Minute),NextUpdate:now.Add(time.Minute)}
  if mode=="revoked"{template.RevokedCertificateEntries=[]x509.RevocationListEntry{{SerialNumber:big.NewInt(2),RevocationTime:now.Add(-time.Minute)}}}
  if mode=="expired"{template.NextUpdate=now}
  if mode=="future"{template.ThisUpdate=now.Add(time.Second)}
  if mode=="stale"{template.ThisUpdate=now.Add(-2*time.Hour)}
  if mode=="partial"{template.ExtraExtensions=[]pkix.Extension{{Id:[]int{2,5,29,28},Value:[]byte{0x30,0}}}}
  crl,err:=x509.CreateRevocationList(rand.Reader,template,issuer,key);if err!=nil{t.Fatal(err)}
  if mode=="wrong_signature"{crl[len(crl)-1]^=1}
  calls:=0
  source:=func(ctx context.Context,raw []byte)([]byte,error){
   calls++
   if mode=="source_error"{return nil,ErrInvalid}
   if mode=="mutation"{raw[0]=0}
   return crl,nil
  }
  check,err:=NewSigningCRLCheck(bundle,source,func()time.Time{return now},time.Hour);if err!=nil{t.Fatal(err)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  if mode=="rollback"||mode=="equivocation"||mode=="revoked_rollback"||mode=="advance"{
   template.Number=big.NewInt(2)
   if mode=="revoked_rollback"{template.RevokedCertificateEntries=[]x509.RevocationListEntry{{SerialNumber:big.NewInt(2),RevocationTime:now.Add(-time.Minute)}}}
   crl,err=x509.CreateRevocationList(rand.Reader,template,issuer,key);if err!=nil{t.Fatal(err)}
   first:=check(ctx,policy,chain)
   if (first==nil)!=(mode!="revoked_rollback"){t.Fatal("first checkpoint",first)}
   template.RevokedCertificateEntries=nil
   if mode=="equivocation"{template.NextUpdate=now.Add(2*time.Minute)}else if mode=="advance"{template.Number=big.NewInt(3)}else{template.Number=big.NewInt(1)}
   crl,err=x509.CreateRevocationList(rand.Reader,template,issuer,key);if err!=nil{t.Fatal(err)}
  }
  err=check(ctx,policy,chain)
  success:=mode=="valid"||mode=="mutation"||mode=="advance"
  if (err==nil)!=success{t.Fatalf("success=%v err=%v",success,err)}
  if success&&mode!="advance"&&calls!=1{t.Fatal("source calls")}
  if mode=="cancelled"&&calls!=0{t.Fatal("cancelled source IO")}
 })}
}
