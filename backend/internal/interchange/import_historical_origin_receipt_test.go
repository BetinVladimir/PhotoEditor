package interchange

import (
 "context"
 "bytes"
 "crypto/ed25519"
 "encoding/base64"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestHistoricalOriginReceiptAuthenticatesExactPayload(t *testing.T){
 public,private,err:=ed25519.GenerateKey(nil);if err!=nil{t.Fatal(err)}
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(uuid.NewString()))}}
 issuer:=uuid.New();p:=HistoricalOriginPayload{Schema:"historical-origin-payload/v1",Tenant:uuid.New(),Project:uuid.New(),Workflow:uuid.New(),Run:uuid.New(),Entry:uuid.New(),Target:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:rawSHA([]byte("canvas"))},OriginalSource:ref(),Contract:"FaceLandmarks",Original:ref(),Prepared:ref(),OriginalImage:ref(),OriginalBoxes:func()*v2input.Ref{v:=ref();return &v}(),Model:rawSHA([]byte("model")),Backend:"mps",Mode:"local_canonical"}
 body,err:=canonical.Marshal(p);if err!=nil{t.Fatal(err)}
 receipt:=HistoricalOriginReceipt{Schema:"historical-origin-receipt/v1",Issuer:issuer,Key:"key1",Payload:base64.StdEncoding.EncodeToString(body),Signature:base64.StdEncoding.EncodeToString(ed25519.Sign(private,append([]byte(historicalOriginDomain),body...)))}
 raw,err:=canonical.Marshal(receipt);if err!=nil{t.Fatal(err)}
 actual,err:=VerifyHistoricalOriginReceipt(raw,issuer,"key1",public);actualBody,_:=canonical.Marshal(actual);if err!=nil||!bytes.Equal(actualBody,body){t.Fatal("valid receipt rejected",err)}
 if _,err=VerifyHistoricalOriginReceipt(raw,uuid.New(),"key1",public);err==nil{t.Fatal("other issuer accepted")}
 p.Entry=uuid.New();body,_=canonical.Marshal(p);receipt.Payload=base64.StdEncoding.EncodeToString(body);raw,_=canonical.Marshal(receipt)
 if _,err=VerifyHistoricalOriginReceipt(raw,issuer,"key1",public);err==nil{t.Fatal("entry substitution accepted")}
}
func TestHistoricalOriginIssuerRequiresIndependentAuthority(t *testing.T){
 _,private,err:=ed25519.GenerateKey(nil);if err!=nil{t.Fatal(err)}
 issuer:=HistoricalOriginIssuer{Signer:private,Issuer:uuid.New(),Project:uuid.New(),KeyID:"key1"}
 raw:=[]byte(`{}`);ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}
 if _,err=issuer.Issue(context.Background(),"WorkingImage",ref,raw);err==nil{t.Fatal("signing key alone authorized archive")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=issuer.Issue(ctx,"WorkingImage",ref,raw);err!=context.Canceled{t.Fatal("cancelled issuance",err)}
}
