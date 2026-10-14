package v2draft

import (
 "encoding/json"
 "os"
 "testing"
 "crypto/sha256"
 "encoding/hex"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestDefectRepairParamsExposeTypedInlineProtection(t *testing.T) {
 original,err:=os.ReadFile("../review/testdata/defect-repair-params-python.json");if err!=nil {t.Fatal(err)}
 for _,mode:=range []string{"valid","not-protected","missing-image","unknown","mask-chain","null-mask","bad-ref"} {t.Run(mode,func(t *testing.T){
  var doc map[string]any
  if err:=json.Unmarshal(original,&doc);err!=nil {t.Fatal(err)}
  switch mode {
  case "not-protected":doc["protected"].(map[string]any)["protected"]=false
  case "missing-image":delete(doc,"image")
  case "unknown":doc["extra"]=true
  case "mask-chain":doc["protected"].(map[string]any)["transform_chain"]=map[string]any{}
  case "null-mask":doc["protected"]=nil
  case "bad-ref":doc["image"].(map[string]any)["sha256"]="invalid"
  }
  raw,err:=canonical.Marshal(doc);if err!=nil {t.Fatal(err)}
  digest:=sha256.Sum256(raw)
  parent:=v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(digest[:])}
  children,err:=DocumentChildren(raw,"Params",parent)
  if mode!="valid" {if err==nil {t.Fatal("invalid repair Params accepted")};return}
  if err!=nil||len(children)!=5 {t.Fatal("repair closure",children,err)}
  contracts:=[]string{"WorkingImage","DefectCandidates","MaskPixels","ResolvedStyle","DefectReview"}
  paths:=[]string{"Document.image","Document.proposal","Document.protected.artifact","Document.resolved","Document.review"}
  for i,child:=range children {
   if child.Contract!=contracts[i]||child.Path!=paths[i]||child.ParentRef!=parent {t.Fatal("incorrect child",i,child)}
  }
  if children[0].CanvasHash==""||children[2].CanvasHash!=children[0].CanvasHash {t.Fatal("lost inline geometry")}
 })}
}
