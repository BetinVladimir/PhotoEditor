package provenance

import (
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
)

type SigningCRLEndpoint struct{IssuerSHA,URL,ServerSHA string}
type signingCRLClient struct{client *http.Client;url string;verify func(tls.ConnectionState)error}
type HTTPSigningCRLSource struct{issuers map[string]signingCRLClient}

func NewHTTPSigningCRLSource(endpoints []SigningCRLEndpoint,roots *x509.CertPool,timeout time.Duration)(*HTTPSigningCRLSource,error){
 if roots==nil||len(endpoints)<1||len(endpoints)>64||timeout<time.Second||timeout>30*time.Second{return nil,ErrInvalid}
 source:=&HTTPSigningCRLSource{issuers:map[string]signingCRLClient{}}
 for _,config:=range endpoints{
  endpoint,err:=url.Parse(config.URL)
  if err!=nil||len(config.URL)>2048||endpoint.Scheme!="https"||endpoint.Hostname()==""||endpoint.User!=nil||endpoint.Opaque!=""||endpoint.RawQuery!=""||endpoint.ForceQuery||endpoint.Fragment!=""||endpoint.RawPath!=""||!digest(config.IssuerSHA)||!digest(config.ServerSHA){source.Close();return nil,ErrInvalid}
  if _,exists:=source.issuers[config.IssuerSHA];exists{source.Close();return nil,ErrInvalid}
  serverSHA:=config.ServerSHA
  verify:=func(state tls.ConnectionState)error{
   if state.Version<tls.VersionTLS13||len(state.PeerCertificates)==0||len(state.VerifiedChains)==0{return ErrInvalid}
   sum:=sha256.Sum256(state.PeerCertificates[0].Raw);if hex.EncodeToString(sum[:])!=serverSHA{return ErrInvalid}
   now:=time.Now();for _,cert:=range state.VerifiedChains[0]{if now.Before(cert.NotBefore)||!now.Before(cert.NotAfter){return ErrInvalid}};return nil
  }
  transport:=&http.Transport{Proxy:nil,DisableCompression:true,DialContext:(&net.Dialer{Timeout:timeout}).DialContext,TLSClientConfig:&tls.Config{RootCAs:roots.Clone(),MinVersion:tls.VersionTLS13,VerifyConnection:verify},TLSHandshakeTimeout:timeout,ResponseHeaderTimeout:timeout,IdleConnTimeout:30*time.Second,MaxIdleConns:1,MaxIdleConnsPerHost:1,MaxConnsPerHost:2,MaxResponseHeaderBytes:8192}
  client:=&http.Client{Transport:transport,Timeout:timeout,CheckRedirect:func(*http.Request,[]*http.Request)error{return ErrInvalid}}
  source.issuers[config.IssuerSHA]=signingCRLClient{client,endpoint.String(),verify}
 }
 return source,nil
}
func(s *HTTPSigningCRLSource)Close(){if s!=nil{for _,entry:=range s.issuers{entry.client.CloseIdleConnections()}}}
func(s *HTTPSigningCRLSource)ReadCRL(ctx context.Context,issuerDER []byte)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s==nil||len(issuerDER)<1||len(issuerDER)>1<<20{return nil,ErrInvalid}
 if _,err:=x509.ParseCertificate(issuerDER);err!=nil{return nil,ErrInvalid}
 sum:=sha256.Sum256(issuerDER);entry,ok:=s.issuers[hex.EncodeToString(sum[:])];if !ok{return nil,ErrInvalid}
 request,err:=http.NewRequestWithContext(ctx,http.MethodGet,entry.url,nil);if err!=nil{return nil,err}
 request.Header.Set("Accept","application/pkix-crl")
 response,err:=entry.client.Do(request);if err!=nil{if response!=nil&&response.Body!=nil{_ = response.Body.Close()};return nil,err};defer response.Body.Close()
 if response.StatusCode!=http.StatusOK||response.TLS==nil||entry.verify(*response.TLS)!=nil||len(response.Header.Values("Content-Type"))!=1||response.Header.Get("Content-Type")!="application/pkix-crl"||len(response.Header.Values("Content-Encoding"))!=0||response.ContentLength>2<<20{return nil,ErrInvalid}
 raw,err:=io.ReadAll(io.LimitReader(response.Body,(2<<20)+1));if err!=nil{return nil,err}
 if err=ctx.Err();err!=nil{return nil,err};if len(raw)<1||len(raw)>2<<20{return nil,ErrInvalid}
 return raw,nil // Cryptographic CRL verification belongs to NewSigningCRLCheck.
}
