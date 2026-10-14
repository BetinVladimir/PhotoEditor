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
 "math/big"
 "net"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync/atomic"
 "testing"
 "time"
 "photoeditor/backend/internal/canonical"
)
func operatorMTLSFixture(t *testing.T)(tls.Certificate,tls.Certificate,*x509.CertPool,string){
 t.Helper();now:=time.Now();public,private,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
 root:=&x509.Certificate{SerialNumber:big.NewInt(1),Subject:pkix.Name{CommonName:"unit CA"},NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),IsCA:true,BasicConstraintsValid:true,KeyUsage:x509.KeyUsageCertSign}
 rootDER,err:=x509.CreateCertificate(rand.Reader,root,root,public,private);if err!=nil{t.Fatal(err)};root,err=x509.ParseCertificate(rootDER);if err!=nil{t.Fatal(err)};roots:=x509.NewCertPool();roots.AddCert(root)
 issue:=func(serial int64,usage x509.ExtKeyUsage)tls.Certificate{t.Helper();pub,key,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)};cert:=&x509.Certificate{SerialNumber:big.NewInt(serial),Subject:pkix.Name{CommonName:"unit transport"},NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),KeyUsage:x509.KeyUsageDigitalSignature,ExtKeyUsage:[]x509.ExtKeyUsage{usage},IPAddresses:[]net.IP{net.ParseIP("127.0.0.1")}};der,err:=x509.CreateCertificate(rand.Reader,cert,root,pub,private);if err!=nil{t.Fatal(err)};return tls.Certificate{Certificate:[][]byte{der,rootDER},PrivateKey:key}}
 server,client:=issue(2,x509.ExtKeyUsageServerAuth),issue(3,x509.ExtKeyUsageClientAuth);sum:=sha256.Sum256(server.Certificate[0]);return server,client,roots,hex.EncodeToString(sum[:])
}
func TestOperatorHTTPChallengeRequiresPinnedMTLSAndClosedProtocol(t *testing.T){
 for _,mode:=range []string{"valid","pin","redirect","status","extra","oversize","encoding","uppercase","noncanonical","payload","cancelled"}{t.Run(mode,func(t *testing.T){
  serverCert,clientCert,roots,pin:=operatorMTLSFixture(t);var calls atomic.Int32
  server:=httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   calls.Add(1);if r.TLS==nil||len(r.TLS.VerifiedChains)==0||len(r.TLS.PeerCertificates)==0||r.Method!="POST"||r.URL.Path!="/internal/v1/operator-challenge"{t.Error("transport not authenticated or exact")}
   var body struct{Schema int `json:"schema_version"`;Handle string `json:"key_handle"`;Algorithm string `json:"algorithm"`;Payload string `json:"payload_hex"`};if err:=json.NewDecoder(r.Body).Decode(&body);err!=nil||body.Schema!=1||body.Handle!="key"||body.Algorithm!="ED25519"{t.Error("wrong request",err)}
   if mode=="redirect"{http.Redirect(w,r,"https://forbidden.invalid/",http.StatusTemporaryRedirect);return};if mode=="status"{w.WriteHeader(503);return}
   w.Header().Set("Content-Type","application/json");if mode=="encoding"{w.Header().Set("Content-Encoding","gzip")}
   var raw []byte
   switch mode{case "extra":raw=[]byte(`{"extra":true,"schema_version":1,"signature_hex":"abcd"}`);case "oversize":raw=[]byte(strings.Repeat("x",4097));case "uppercase":raw=[]byte(`{"schema_version":1,"signature_hex":"ABCD"}`);case "noncanonical":raw=[]byte(`{ "schema_version": 1, "signature_hex": "abcd" }`);default:raw=[]byte(`{"schema_version":1,"signature_hex":"abcd"}`)}
   _,_=w.Write(raw)
  }));server.TLS=&tls.Config{Certificates:[]tls.Certificate{serverCert},ClientCAs:roots,ClientAuth:tls.RequireAndVerifyClientCert,MinVersion:tls.VersionTLS13};server.StartTLS();defer server.Close()
  if mode=="pin"{pin=strings.Repeat("f",64)}
  client,err:=NewHTTPSigningChallengeSigner(server.URL,roots,clientCert,pin,time.Second);if err!=nil{t.Fatal(err)};defer client.Close()
  payload,err:=canonical.Marshal(map[string]string{"domain":"photoeditor.signing-operator-possession/v1","policy_sha256":strings.Repeat("a",64),"nonce":strings.Repeat("b",64)});if err!=nil{t.Fatal(err)};if mode=="payload"{payload=[]byte(`{}`)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  signature,err:=client.SignOperatorChallenge(ctx,"key","ED25519",payload)
  if mode=="valid"{if err!=nil||hex.EncodeToString(signature)!="abcd"||calls.Load()!=1{t.Fatal("valid pinned mTLS refused",err)};return}
  if err==nil||signature!=nil{t.Fatal("invalid transport response accepted",mode)}
  if (mode=="pin"||mode=="payload"||mode=="cancelled")&&calls.Load()!=0{t.Fatal("denied request reached operator")}
 })}
}
func TestOperatorHTTPTransportRejectsUntrustedConfiguration(t *testing.T){
 _,identity,roots,pin:=operatorMTLSFixture(t)
 for _,origin:=range []string{"http://localhost","https://user:password@localhost","https://localhost/path","https://localhost?query","https://localhost#fragment"}{if _,err:=NewHTTPSigningChallengeSigner(origin,roots,identity,pin,time.Second);err==nil{t.Fatal("unsafe origin accepted",origin)}}
 if _,err:=NewHTTPSigningChallengeSigner("https://localhost",nil,identity,pin,time.Second);err==nil{t.Fatal("missing trust accepted")}
 if _,err:=NewHTTPSigningChallengeSigner("https://localhost",roots,tls.Certificate{},pin,time.Second);err==nil{t.Fatal("missing mTLS identity accepted")}
}
