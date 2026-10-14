package provenance
import (
 "context"
 "crypto"
 "crypto/ecdsa"
 "crypto/ed25519"
 "crypto/elliptic"
 "crypto/rand"
 "crypto/rsa"
 "crypto/sha256"
 "crypto/sha512"
 "crypto/x509"
 "crypto/x509/pkix"
 "encoding/hex"
 "encoding/pem"
 "errors"
 "math/big"
 "testing"
 "time"
 "github.com/google/uuid"
)
type operatorChallengeFunc func(context.Context,string,string,[]byte)([]byte,error)
func(f operatorChallengeFunc)SignOperatorChallenge(c context.Context,k,a string,p []byte)([]byte,error){return f(c,k,a,p)}
type operatorTrustFunc func(context.Context,SigningPolicy,[]byte)error
func(f operatorTrustFunc)CheckSigningTrust(c context.Context,p SigningPolicy,b []byte)error{return f(c,p,b)}
func operatorChallengeFixture(t *testing.T,algorithm string)(crypto.Signer,[]byte,SigningPolicy,time.Time){
 t.Helper();var key crypto.Signer;var err error
 switch algorithm{case "PS256":key,err=rsa.GenerateKey(rand.Reader,2048);case "ES256":key,err=ecdsa.GenerateKey(elliptic.P256(),rand.Reader);case "ES384":key,err=ecdsa.GenerateKey(elliptic.P384(),rand.Reader);case "ES512":key,err=ecdsa.GenerateKey(elliptic.P521(),rand.Reader);default:_,key,err=ed25519.GenerateKey(rand.Reader)};if err!=nil{t.Fatal(err)}
 now:=time.Date(2026,10,11,12,0,0,0,time.UTC);template:=&x509.Certificate{SerialNumber:big.NewInt(1),Subject:pkix.Name{CommonName:"unit-only"},NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),KeyUsage:x509.KeyUsageDigitalSignature}
 der,err:=x509.CreateCertificate(rand.Reader,template,template,key.Public(),key);if err!=nil{t.Fatal(err)};chain:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der})
 pin:=func(raw []byte)string{sum:=sha256.Sum256(raw);return hex.EncodeToString(sum[:])}
 policy:=SigningPolicy{Schema:1,ID:uuid.New(),Revision:"1",SDKLockSHA:pin([]byte("sdk")),KeyHandle:"unit-handle",CertificateChainSHA:pin(chain),TimestampMode:"off",AllowedMIME:[]string{"image/png"},TrustBundleSHA:pin([]byte("trust")),RevocationMode:"required"}
 return key,chain,policy,now
}
func TestOperatorChallengeVerifiesAllInstalledKeyFamilies(t *testing.T){
 for _,algorithm:=range []string{"PS256","ES256","ES384","ES512","ED25519"}{t.Run(algorithm,func(t *testing.T){
  key,chain,policy,now:=operatorChallengeFixture(t,algorithm);trustCalls,signCalls:=0,0;var previous string
  signer:=operatorChallengeFunc(func(_ context.Context,handle,alg string,payload []byte)([]byte,error){
   signCalls++;if handle!=policy.KeyHandle||alg!=algorithm||string(payload)==previous{t.Fatal("wrong handle/algorithm or reused challenge")};previous=string(payload)
   var hashed []byte;var opts crypto.SignerOpts
   switch alg{case "PS256","ES256":h:=sha256.Sum256(payload);hashed=h[:];opts=crypto.SHA256;case "ES384":h:=sha512.Sum384(payload);hashed=h[:];opts=crypto.SHA384;case "ES512":h:=sha512.Sum512(payload);hashed=h[:];opts=crypto.SHA512;default:hashed=payload;opts=crypto.Hash(0)}
   if alg=="PS256"{opts=&rsa.PSSOptions{SaltLength:rsa.PSSSaltLengthEqualsHash,Hash:crypto.SHA256}}
   sig,err:=key.Sign(rand.Reader,hashed,opts);payload[0]='!';return sig,err
  })
  trust:=operatorTrustFunc(func(_ context.Context,p SigningPolicy,b []byte)error{trustCalls++;if p.CertificateChainSHA!=policy.CertificateChainSHA||string(b)!=string(chain){t.Fatal("trust input changed")};p.AllowedMIME[0]="mutated";b[0]='!';return nil})
  authority,err:=NewChallengeSigningOperatorAuthority(chain,signer,trust,func()time.Time{return now});if err!=nil{t.Fatal(err)}
  for i:=0;i<2;i++{if err:=authority.CheckSigningOperator(context.Background(),policy);err!=nil{t.Fatal(err)}}
  if signCalls!=2||trustCalls!=4||policy.AllowedMIME[0]!="image/png"{t.Fatal("fresh proof/repeated trust/ownership missing")}
 })}
}
func TestOperatorChallengeRejectsReplayRevocationExpiryAndBadSignatures(t *testing.T){
 for _,mode:=range []string{"signature","replay","revoked","expired","chain","cancelled"}{t.Run(mode,func(t *testing.T){
  key,chain,policy,now:=operatorChallengeFixture(t,"ED25519");sentinel:=errors.New("live trust revoked");calls:=0;var previous []byte
  signer:=operatorChallengeFunc(func(_ context.Context,_ ,_ string,payload []byte)([]byte,error){if mode=="signature"{return []byte("bad"),nil};if mode=="replay"&&previous!=nil{return previous,nil};sig,err:=key.Sign(rand.Reader,payload,crypto.Hash(0));previous=sig;return sig,err})
  trust:=operatorTrustFunc(func(context.Context,SigningPolicy,[]byte)error{calls++;if mode=="revoked"&&calls==2{return sentinel};return nil})
  authority,err:=NewChallengeSigningOperatorAuthority(chain,signer,trust,func()time.Time{if mode=="expired"{return now.Add(time.Hour)};return now});if err!=nil{t.Fatal(err)}
  if mode=="chain"{policy.CertificateChainSHA=policy.SDKLockSHA}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  if mode=="replay"{if err:=authority.CheckSigningOperator(ctx,policy);err!=nil{t.Fatal(err)}}
  err=authority.CheckSigningOperator(ctx,policy);if err==nil{t.Fatal("invalid operator admitted",mode)}
  if mode=="revoked"&&!errors.Is(err,sentinel){t.Fatal("revocation lost",err)}
  if mode=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
 })}
}
