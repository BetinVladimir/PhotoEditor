package provenance

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "crypto/tls"
 "crypto/x509"
 "crypto/x509/pkix"
 "encoding/hex"
 "encoding/json"
 "encoding/pem"
 "math/big"
 "net/http"
 "net/http/httptest"
 "sync/atomic"
 "testing"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

// Actual TLS and public-key checks; synthetic CA/native callback do not qualify
// production SDK, external hardware key, revocation or timestamp services.
func TestInstalledOperatorRealMTLSAndCertificateProof(t *testing.T){
 for _,mode:=range []string{"valid","signature","native_revoked","required_revocation","required_timestamp","trust_pin","closed","cancelled"}{t.Run(mode,func(t *testing.T){
  now:=time.Now().UTC()
  caPublic,caPrivate,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
  ca:=&x509.Certificate{SerialNumber:big.NewInt(1),Subject:pkix.Name{CommonName:"unit signing CA"},NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),IsCA:true,BasicConstraintsValid:true,KeyUsage:x509.KeyUsageCertSign}
  caDER,err:=x509.CreateCertificate(rand.Reader,ca,ca,caPublic,caPrivate);if err!=nil{t.Fatal(err)}
  public,private,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
  leaf:=&x509.Certificate{SerialNumber:big.NewInt(2),NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),KeyUsage:x509.KeyUsageDigitalSignature,ExtKeyUsage:[]x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}}
  leafDER,err:=x509.CreateCertificate(rand.Reader,leaf,ca,public,caPrivate);if err!=nil{t.Fatal(err)}
  chain:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:leafDER})
  bundle:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:caDER})
  pin:=func(raw []byte)string{sum:=sha256.Sum256(raw);return hex.EncodeToString(sum[:])}
  policy:=SigningPolicy{Schema:1,ID:uuid.New(),Revision:"1",SDKLockSHA:pin([]byte("unit SDK")),KeyHandle:"external-key",CertificateChainSHA:pin(chain),TrustBundleSHA:pin(bundle),AllowedMIME:[]string{"image/png"},RevocationMode:"off",TimestampMode:"off"}
  if mode=="required_revocation"{policy.RevocationMode="required"}
  if mode=="required_timestamp"{policy.TimestampMode="required";service:="tsa";policy.TimestampServiceID=&service}
  if mode=="trust_pin"{policy.TrustBundleSHA=policy.SDKLockSHA}
  policyRaw,err:=canonical.Marshal(policy);if err!=nil{t.Fatal(err)}
  serverCertificate,identity,roots,serverPin:=operatorMTLSFixture(t)
  var signCalls,nativeCalls atomic.Int32
  server:=httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   signCalls.Add(1)
   if r.TLS==nil||len(r.TLS.VerifiedChains)==0||r.URL.Path!="/internal/v1/operator-challenge"{t.Error("unverified challenge transport")}
   var request struct{Schema int `json:"schema_version"`;Handle string `json:"key_handle"`;Algorithm string `json:"algorithm"`;Payload string `json:"payload_hex"`}
   if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil{t.Error(err);w.WriteHeader(400);return}
   payload,err:=hex.DecodeString(request.Payload);if err!=nil{t.Error(err);w.WriteHeader(400);return}
   var challenge struct{Domain string `json:"domain"`;Policy string `json:"policy_sha256"`;Nonce string `json:"nonce"`}
   if json.Unmarshal(payload,&challenge)!=nil||challenge.Domain!="photoeditor.signing-operator-possession/v1"||challenge.Policy!=pin(policyRaw)||!digest(challenge.Nonce)||request.Handle!=policy.KeyHandle||request.Algorithm!="ED25519"{t.Error("unbound challenge");w.WriteHeader(400);return}
   signature:=ed25519.Sign(private,payload)
   if mode=="signature"{signature[0]^=1}
   body,err:=canonical.Marshal(map[string]any{"schema_version":1,"signature_hex":hex.EncodeToString(signature)});if err!=nil{t.Error(err);return}
   w.Header().Set("Content-Type","application/json");_,_=w.Write(body)
  }))
  server.TLS=&tls.Config{Certificates:[]tls.Certificate{serverCertificate},ClientCAs:roots,ClientAuth:tls.RequireAndVerifyClientCert,MinVersion:tls.VersionTLS13}
  server.StartTLS();defer server.Close()
  native:=func(context.Context,SigningPolicy,[]byte)error{
   calls:=nativeCalls.Add(1)
   if mode=="native_revoked"&&calls>1{return ErrInvalid}
   return nil
  }
  authority,err:=NewInstalledChallengeSigningAuthority(SigningOperatorInstallation{CertificateChain:chain,TrustBundle:bundle,CertificateUsages:[]x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},ChallengeOrigin:server.URL,ChallengeServerSHA:serverPin,ChallengeRoots:roots,ChallengeIdentity:identity,Timeout:time.Second,Native:native,Clock:func()time.Time{return now}})
  if err!=nil{t.Fatal(err)};defer authority.Close()
  if mode=="closed"{authority.Close()}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=authority.CheckSigningOperator(ctx,policy)
  if (err==nil)!=(mode=="valid"){t.Fatalf("mode=%s err=%v",mode,err)}
  if mode=="valid"&&(signCalls.Load()!=1||nativeCalls.Load()!=2){t.Fatal("missing real proof or repeated native check",signCalls.Load(),nativeCalls.Load())}
  if (mode=="closed"||mode=="cancelled"||mode=="required_revocation"||mode=="required_timestamp"||mode=="trust_pin")&&signCalls.Load()!=0{t.Fatal("refusal reached external key")}
 })}
}
