package provenance
import (
 "bytes"
 "context"
 "crypto/sha256"
 "crypto/tls"
 "crypto/x509"
 "encoding/hex"
 "io"
 "net"
 "net/http"
 "net/url"
 "time"
 "photoeditor/backend/internal/canonical"
)

type HTTPSigningChallengeSigner struct{client *http.Client;endpoint string;verify func(tls.ConnectionState)error}
// origin/credentials/pin are trusted process configuration. PrivateKey here is
// the API's mTLS identity, never the image signing key held by the signer.
func NewHTTPSigningChallengeSigner(origin string,roots *x509.CertPool,identity tls.Certificate,serverSHA string,timeout time.Duration)(*HTTPSigningChallengeSigner,error){
 endpoint,err:=url.Parse(origin)
 if err!=nil||endpoint.Scheme!="https"||endpoint.Hostname()==""||endpoint.User!=nil||endpoint.Opaque!=""||endpoint.RawQuery!=""||endpoint.ForceQuery||endpoint.Fragment!=""||endpoint.RawPath!=""||(endpoint.Path!=""&&endpoint.Path!="/")||roots==nil||len(identity.Certificate)==0||identity.PrivateKey==nil||!digest(serverSHA)||timeout<time.Second||timeout>30*time.Second{return nil,ErrInvalid}
 endpoint.Path="/internal/v1/operator-challenge"
 owned:=identity;owned.Certificate=make([][]byte,len(identity.Certificate));for i,cert:=range identity.Certificate{owned.Certificate[i]=append([]byte(nil),cert...)}
 owned.OCSPStaple=append([]byte(nil),identity.OCSPStaple...);owned.SignedCertificateTimestamps=nil;owned.Leaf=nil
 verify:=func(state tls.ConnectionState)error{
  if state.Version<tls.VersionTLS13||len(state.PeerCertificates)==0||len(state.VerifiedChains)==0{return ErrInvalid}
  sum:=sha256.Sum256(state.PeerCertificates[0].Raw);if hex.EncodeToString(sum[:])!=serverSHA{return ErrInvalid}
  now:=time.Now();for _,cert:=range state.VerifiedChains[0]{if now.Before(cert.NotBefore)||!now.Before(cert.NotAfter){return ErrInvalid}}
  return nil
 }
 transport:=&http.Transport{Proxy:nil,DisableCompression:true,DialContext:(&net.Dialer{Timeout:timeout,KeepAlive:30*time.Second}).DialContext,TLSClientConfig:&tls.Config{RootCAs:roots.Clone(),Certificates:[]tls.Certificate{owned},MinVersion:tls.VersionTLS13,VerifyConnection:verify},TLSHandshakeTimeout:timeout,ResponseHeaderTimeout:timeout,IdleConnTimeout:30*time.Second,MaxIdleConns:2,MaxIdleConnsPerHost:2,MaxConnsPerHost:4,MaxResponseHeaderBytes:8192}
 client:=&http.Client{Transport:transport,Timeout:timeout,CheckRedirect:func(*http.Request,[]*http.Request)error{return ErrInvalid}}
 return &HTTPSigningChallengeSigner{client:client,endpoint:endpoint.String(),verify:verify},nil
}
func(s *HTTPSigningChallengeSigner)Close(){if s!=nil&&s.client!=nil{s.client.CloseIdleConnections()}}
func(s *HTTPSigningChallengeSigner)SignOperatorChallenge(ctx context.Context,handle,algorithm string,payload []byte)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s==nil||s.client==nil||s.verify==nil||!text(handle,200){return nil,ErrInvalid}
 switch algorithm{case "PS256","ES256","ES384","ES512","ED25519":default:return nil,ErrInvalid}
 var challenge struct{Domain string `json:"domain"`;Policy string `json:"policy_sha256"`;Nonce string `json:"nonce"`}
 if decode(payload,&challenge,4096)!=nil||challenge.Domain!="photoeditor.signing-operator-possession/v1"||!digest(challenge.Policy)||!digest(challenge.Nonce){return nil,ErrInvalid}
 body,err:=canonical.Marshal(struct{Schema int `json:"schema_version"`;Handle string `json:"key_handle"`;Algorithm string `json:"algorithm"`;Payload string `json:"payload_hex"`}{1,handle,algorithm,hex.EncodeToString(payload)});if err!=nil{return nil,err}
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,s.endpoint,bytes.NewReader(body));if err!=nil{return nil,err};request.Header.Set("Content-Type","application/json");request.Header.Set("Accept","application/json")
 response,err:=s.client.Do(request);if err!=nil{if response!=nil&&response.Body!=nil{_ = response.Body.Close()};return nil,err};defer response.Body.Close()
 if response.StatusCode!=http.StatusOK||response.TLS==nil||s.verify(*response.TLS)!=nil||response.Header.Get("Content-Type")!="application/json"||response.Header.Get("Content-Encoding")!=""||response.ContentLength>4096{return nil,ErrInvalid}
 raw,err:=io.ReadAll(io.LimitReader(response.Body,4097));if err!=nil{return nil,err};if err:=ctx.Err();err!=nil{return nil,err}
 var result struct{Schema int `json:"schema_version"`;Signature string `json:"signature_hex"`}
 if decode(raw,&result,4096)!=nil||result.Schema!=1||len(result.Signature)<2||len(result.Signature)>2048{return nil,ErrInvalid}
 signature,err:=hex.DecodeString(result.Signature);if err!=nil||hex.EncodeToString(signature)!=result.Signature{return nil,ErrInvalid};return signature,nil
}
var _ SigningChallengeSigner=(*HTTPSigningChallengeSigner)(nil)
