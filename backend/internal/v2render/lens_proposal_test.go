package v2render

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
func TestLensProposalRejectsMissingAuthorityAndCancellation(t *testing.T){
 service:=LensProposalService{}
 if _,err:=service.Describe(context.Background(),auth.Identity{},uuid.New(),uuid.New(),uuid.New(),uuid.New());err==nil{t.Fatal("uninstalled proposal exposed")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=service.Describe(ctx,auth.Identity{},uuid.New(),uuid.New(),uuid.New(),uuid.New());err!=context.Canceled{t.Fatal("cancelled description continued",err)}
}
func TestLensOriginalDocumentBytesAreExactCanonicalAndPinned(t *testing.T){
 raw:=[]byte(`{"a":1}`);sum:=sha256.Sum256(raw);ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}
 if err:=verifyLensDocumentBytes(raw,ref);err!=nil{t.Fatal(err)}
 for _,changed:=range [][]byte{[]byte(`{"a":2}`),[]byte(`{ "a":1}`),[]byte(`{"a":1,"a":1}`)}{
  if err:=verifyLensDocumentBytes(changed,ref);err==nil{t.Fatal("substituted original bytes accepted")}
 }
}
