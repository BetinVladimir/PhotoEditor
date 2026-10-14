package provenance

import (
 "bytes"
 "context"
 "crypto/sha256"
 "crypto/x509"
 "encoding/hex"
 "encoding/pem"
 "time"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Native, revocation and timestamp checks are independent live services, not
// boolean configuration flags. All receive owned policy and certificate bytes.
type SigningTrustCheck func(context.Context, SigningPolicy, []byte) error

type CertificateSigningTrustAuthority struct {
 bundle []byte
 roots *x509.CertPool
 usages []x509.ExtKeyUsage
 native, revocation, timestamp SigningTrustCheck
 clock func() time.Time
}

func signingTrustCertificates(raw []byte) ([]*x509.Certificate,error) {
 if len(raw)<1 || len(raw)>1<<20 { return nil,ErrInvalid }
 rest:=raw
 certs:=[]*x509.Certificate{}
 for len(bytes.TrimSpace(rest))>0 {
  rest=bytes.TrimSpace(rest)
  if !bytes.HasPrefix(rest,[]byte("-----BEGIN CERTIFICATE-----")) {return nil,ErrInvalid}
  block,next:=pem.Decode(rest)
  if block==nil || block.Type!="CERTIFICATE" || len(block.Headers)!=0 || len(certs)>=32 {return nil,ErrInvalid}
  cert,err:=x509.ParseCertificate(block.Bytes);if err!=nil{return nil,ErrInvalid}
  certs=append(certs,cert);rest=next
 }
 if len(certs)==0{return nil,ErrInvalid};return certs,nil
}

// EKUs are trusted installation input and must match the qualified certificate
// profile. ExtKeyUsageAny is deliberately forbidden; SDK profile checks remain
// mandatory in native. No system root fallback or network issuer discovery.
func NewCertificateSigningTrustAuthority(bundle []byte,usages []x509.ExtKeyUsage,native,revocation,timestamp SigningTrustCheck,clock func()time.Time)(*CertificateSigningTrustAuthority,error){
 if native==nil || clock==nil || len(usages)<1 || len(usages)>16{return nil,ErrInvalid}
 seen:=map[x509.ExtKeyUsage]bool{}
 for _,usage:=range usages{if usage<=x509.ExtKeyUsageAny || usage>x509.ExtKeyUsageMicrosoftKernelCodeSigning || seen[usage]{return nil,ErrInvalid};seen[usage]=true}
 owned:=append([]byte(nil),bundle...)
 certs,err:=signingTrustCertificates(owned);if err!=nil{return nil,err}
 roots:=x509.NewCertPool()
 for _,cert:=range certs{
  if !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign==0{return nil,ErrInvalid}
  roots.AddCert(cert)
 }
 return &CertificateSigningTrustAuthority{owned,roots,append([]x509.ExtKeyUsage(nil),usages...),native,revocation,timestamp,clock},nil
}

func(a *CertificateSigningTrustAuthority)CheckSigningTrust(ctx context.Context,policy SigningPolicy,chain []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if a==nil || a.roots==nil || a.native==nil || a.clock==nil{return ErrInvalid}
 raw,err:=canonical.Marshal(policy);if err!=nil{return err}
 sum:=sha256.Sum256(raw);ref:=v2input.Ref{ArtifactID:policy.ID,SHA256:hex.EncodeToString(sum[:])}
 if _,err=DecodeSigningPolicy(raw,ref,policy.ID);err!=nil{return err}
 ownedChain:=append([]byte(nil),chain...)
 chainSum,bundleSum:=sha256.Sum256(ownedChain),sha256.Sum256(a.bundle)
 if hex.EncodeToString(chainSum[:])!=policy.CertificateChainSHA || hex.EncodeToString(bundleSum[:])!=policy.TrustBundleSHA{return ErrInvalid}
 certs,err:=signingTrustCertificates(ownedChain);if err!=nil{return err}
 verify:=func()error{
  if err:=ctx.Err();err!=nil{return err}
  now:=a.clock();if now.IsZero(){return ErrInvalid}
  for _,cert:=range certs{if now.Before(cert.NotBefore)||!now.Before(cert.NotAfter){return ErrInvalid}}
  if certs[0].IsCA || certs[0].KeyUsage&x509.KeyUsageDigitalSignature==0{return ErrInvalid}
  intermediates:=x509.NewCertPool();for _,cert:=range certs[1:]{intermediates.AddCert(cert)}
  _,err:=certs[0].Verify(x509.VerifyOptions{Roots:a.roots,Intermediates:intermediates,CurrentTime:now,KeyUsages:a.usages})
  if err!=nil{return ErrInvalid};return ctx.Err()
 }
 if err=verify();err!=nil{return err}
 invoke:=func(check SigningTrustCheck)error{
  if check==nil{return ErrInvalid}
  owned,err:=DecodeSigningPolicy(append([]byte(nil),raw...),ref,policy.ID);if err!=nil{return err}
  if err=check(ctx,owned,append([]byte(nil),ownedChain...));err!=nil{return err}
  return verify()
 }
 if err=invoke(a.native);err!=nil{return err}
 if policy.RevocationMode=="required"{if err=invoke(a.revocation);err!=nil{return err}}
 if policy.TimestampMode=="required"{if err=invoke(a.timestamp);err!=nil{return err}}
 return verify()
}
var _ SigningLiveTrustAuthority=(*CertificateSigningTrustAuthority)(nil)
