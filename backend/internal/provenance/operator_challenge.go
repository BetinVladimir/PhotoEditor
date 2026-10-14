package provenance

import (
 "bytes"
 "context"
 "crypto"
 "crypto/ecdsa"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/rsa"
 "crypto/sha256"
 "crypto/sha512"
 "crypto/x509"
 "encoding/hex"
 "encoding/pem"
 "time"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Implemented by an authenticated isolated signer transport. It signs exactly
// payload with the installed handle; it never accepts key material from API.
type SigningChallengeSigner interface{SignOperatorChallenge(context.Context,string,string,[]byte)([]byte,error)}
// Must independently check qualified native/SDK installation, current trust,
// revocation and timestamp policy. Key possession is not a substitute.
type SigningLiveTrustAuthority interface{CheckSigningTrust(context.Context,SigningPolicy,[]byte)error}
type ChallengeSigningOperatorAuthority struct{chain []byte;certificates []*x509.Certificate;signer SigningChallengeSigner;trust SigningLiveTrustAuthority;clock func()time.Time}
func NewChallengeSigningOperatorAuthority(chain []byte,signer SigningChallengeSigner,trust SigningLiveTrustAuthority,clock func()time.Time)(*ChallengeSigningOperatorAuthority,error){
 if len(chain)<1||len(chain)>1<<20||signer==nil||trust==nil||clock==nil{return nil,ErrInvalid}
 owned:=append([]byte(nil),chain...);remaining:=owned;certificates:=[]*x509.Certificate{}
 for len(bytes.TrimSpace(remaining))>0{
  // No skipped arbitrary preamble, PEM headers or non-certificate blocks.
  remaining=bytes.TrimSpace(remaining);if !bytes.HasPrefix(remaining,[]byte("-----BEGIN CERTIFICATE-----")){return nil,ErrInvalid}
  block,rest:=pem.Decode(remaining);if block==nil||block.Type!="CERTIFICATE"||len(block.Headers)!=0||len(certificates)>=32{return nil,ErrInvalid}
  certificate,err:=x509.ParseCertificate(block.Bytes);if err!=nil{return nil,ErrInvalid};certificates=append(certificates,certificate);remaining=rest
 }
 if len(certificates)==0||certificates[0].KeyUsage&x509.KeyUsageDigitalSignature==0{return nil,ErrInvalid}
 if _,_,err:=signingChallengeAlgorithm(certificates[0]);err!=nil{return nil,err}
 return &ChallengeSigningOperatorAuthority{chain:owned,certificates:certificates,signer:signer,trust:trust,clock:clock},nil
}
func signingChallengeAlgorithm(cert *x509.Certificate)(string,crypto.Hash,error){
 switch key:=cert.PublicKey.(type){
 case *rsa.PublicKey:if key.N.BitLen()<2048||key.N.BitLen()>8192{return "",0,ErrInvalid};return "PS256",crypto.SHA256,nil
 case *ecdsa.PublicKey:
  switch key.Curve.Params().Name{case "P-256":return "ES256",crypto.SHA256,nil;case "P-384":return "ES384",crypto.SHA384,nil;case "P-521":return "ES512",crypto.SHA512,nil}
 case ed25519.PublicKey:if len(key)==ed25519.PublicKeySize{return "ED25519",0,nil}
 }
 return "",0,ErrInvalid
}
func(a *ChallengeSigningOperatorAuthority)CheckSigningOperator(ctx context.Context,policy SigningPolicy)error{
 if err:=ctx.Err();err!=nil{return err}
 if a==nil||a.signer==nil||a.trust==nil||a.clock==nil||len(a.certificates)==0{return ErrInvalid}
 raw,err:=canonical.Marshal(policy);if err!=nil{return err};policySHA:=sha256.Sum256(raw)
 ref:=v2input.Ref{ArtifactID:policy.ID,SHA256:hex.EncodeToString(policySHA[:])}
 if _,err:=DecodeSigningPolicy(raw,ref,policy.ID);err!=nil{return err}
 chainSHA:=sha256.Sum256(a.chain);if hex.EncodeToString(chainSHA[:])!=policy.CertificateChainSHA{return ErrInvalid}
 check:=func()error{
  if err:=ctx.Err();err!=nil{return err};now:=a.clock();if now.IsZero(){return ErrInvalid}
  for _,cert:=range a.certificates{if now.Before(cert.NotBefore)||!now.Before(cert.NotAfter){return ErrInvalid}}
  owned,err:=DecodeSigningPolicy(append([]byte(nil),raw...),ref,policy.ID);if err!=nil{return err}
  if err:=a.trust.CheckSigningTrust(ctx,owned,append([]byte(nil),a.chain...));err!=nil{return err};return ctx.Err()
 }
 if err:=check();err!=nil{return err}
 nonce:=make([]byte,32);if _,err:=rand.Read(nonce);err!=nil{return err}
 payload,err:=canonical.Marshal(struct{Domain string `json:"domain"`;Policy string `json:"policy_sha256"`;Nonce string `json:"nonce"`}{"photoeditor.signing-operator-possession/v1",ref.SHA256,hex.EncodeToString(nonce)});if err!=nil{return err}
 algorithm,hash,err:=signingChallengeAlgorithm(a.certificates[0]);if err!=nil{return err}
 signature,err:=a.signer.SignOperatorChallenge(ctx,policy.KeyHandle,algorithm,append([]byte(nil),payload...));if err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err};if len(signature)<1||len(signature)>1024{return ErrInvalid}
 signature=append([]byte(nil),signature...)
 var hashed []byte
 switch hash{case crypto.SHA256:h:=sha256.Sum256(payload);hashed=h[:];case crypto.SHA384:h:=sha512.Sum384(payload);hashed=h[:];case crypto.SHA512:h:=sha512.Sum512(payload);hashed=h[:]}
 switch key:=a.certificates[0].PublicKey.(type){
 case *rsa.PublicKey:if rsa.VerifyPSS(key,hash,hashed,signature,&rsa.PSSOptions{SaltLength:rsa.PSSSaltLengthEqualsHash,Hash:hash})!=nil{return ErrInvalid}
 case *ecdsa.PublicKey:if !ecdsa.VerifyASN1(key,hashed,signature){return ErrInvalid}
 case ed25519.PublicKey:if !ed25519.Verify(key,payload,signature){return ErrInvalid}
 default:return ErrInvalid
 }
 // Repeat live authority after external key IO. A valid proof must not rescue
 // a policy/certificate/runtime revoked while the challenge was outstanding.
 return check()
}
var _ SigningOperatorAuthority=(*ChallengeSigningOperatorAuthority)(nil)
