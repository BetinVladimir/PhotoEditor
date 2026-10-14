package provenance

import (
 "context"
 "crypto/sha256"
 "crypto/tls"
 "crypto/x509"
 "encoding/hex"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"
)

func TestHTTPSigningCRLSource(t *testing.T){
 for _,mode:=range []string{"valid","pin","status","redirect","encoding","mime","oversize","cancelled","issuer"}{t.Run(mode,func(t *testing.T){
  calls:=0
  server:=httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   calls++
   if r.Method!="GET"||r.Header.Get("Accept")!="application/pkix-crl"{t.Error("request envelope")}
   if mode=="redirect"{w.Header().Set("Location","https://example.invalid/other");w.WriteHeader(302);return}
   if mode=="status"{w.WriteHeader(503);return}
   w.Header().Set("Content-Type","application/pkix-crl")
   if mode=="mime"{w.Header().Set("Content-Type","text/plain")}
   if mode=="encoding"{w.Header().Set("Content-Encoding","gzip")}
   if mode=="oversize"{_,_=w.Write(make([]byte,(2<<20)+1));return}
   _,_=w.Write([]byte("DER fixture; verified by separate CRL layer"))
  }))
  server.TLS=&tls.Config{MinVersion:tls.VersionTLS13};server.StartTLS();defer server.Close()
  cert:=server.Certificate();roots:=x509.NewCertPool();roots.AddCert(cert)
  sum:=sha256.Sum256(cert.Raw);pin:=hex.EncodeToString(sum[:]);serverPin:=pin
  if mode=="pin"{serverPin=hex.EncodeToString(make([]byte,32))}
  source,err:=NewHTTPSigningCRLSource([]SigningCRLEndpoint{{pin,server.URL,serverPin}},roots,2*time.Second);if err!=nil{t.Fatal(err)};defer source.Close()
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  issuer:=cert.Raw;if mode=="issuer"{issuer=[]byte("invalid certificate")}
  raw,err:=source.ReadCRL(ctx,issuer)
  if (err==nil)!=(mode=="valid"){t.Fatalf("mode=%s err=%v",mode,err)}
  if mode=="valid"&&len(raw)==0{t.Fatal("empty response")}
  if (mode=="cancelled"||mode=="issuer")&&calls!=0{t.Fatal("refusal performed IO")}
 })}
}
