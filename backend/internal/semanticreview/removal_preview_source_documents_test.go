package semanticreview

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func removalSourceDocumentFixture(t *testing.T) (RemovalPreviewParams, []byte, []byte, ExposureSourceDocuments) {
 t.Helper()
 raw,err:=os.ReadFile("../review/testdata/working-images-python.json");if err!=nil {t.Fatal(err)}
 var images []json.RawMessage
 if err=json.Unmarshal(raw,&images);err!=nil || len(images)==0 {t.Fatal("missing WorkingImage",err)}
 image,err:=review.ParseWorkingImage(images[0]);if err!=nil {t.Fatal(err)}
 ref:=func(body []byte)v2input.Ref {sum:=sha256.Sum256(body);return v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}}
 marshal:=func(value any)[]byte {body,err:=canonical.Marshal(value);if err!=nil {t.Fatal(err)};return body}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil {t.Fatal(err)}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(images[0]),CanvasHash:canvas}
 mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"manual",Confidence:1,Protected:false}
 maskBody:=marshal(mask);maskRef:=ref(maskBody)
 mask.Protected=true;protectionBody:=marshal(mask)
 p:=RemovalPreviewParams{ExposurePreviewParams:previewParamsFixture(),Kind:"remove_object",ModelID:"big_lama",ContextPixels:64,FeatherPixels:8}
 p.Source=binding;p.Protection=ref(protectionBody)
 transform:=ref([]byte(`{"transform":1}`))
 candidate:=marshal(v2input.SemanticCandidate{SchemaVersion:1,Target:binding,Mask:&maskRef,Transform:&transform,Confidence:1,State:"proposed"})
 intent:=marshal(v2input.SemanticEditIntent{SchemaVersion:1,Kind:"remove_object",Reference:binding,ReferenceMask:maskRef,TargetSnapshotID:p.SnapshotID,Strength:.5,PreservePersons:true})
 p.Candidate=ref(candidate);p.Intent=ref(intent)
 return p,candidate,intent,ExposureSourceDocuments{Source:append([]byte(nil),images[0]...),Reference:append([]byte(nil),images[0]...),Mask:maskBody,Protection:protectionBody}
}

func TestRemovalSourceDocumentsBindSourceProtectionAndIntent(t *testing.T) {
 for _,mode:=range []string{"valid","candidate_bytes","intent_bytes","source_bytes","reference_bytes","mask_bytes","protection_bytes","source_version","source_canvas","candidate_state","intent_kind","snapshot","preserve_persons","unprotected_mask"} {
  t.Run(mode,func(t *testing.T){
   p,candidate,intent,docs:=removalSourceDocumentFixture(t)
   repin:=func(body []byte)string{sum:=sha256.Sum256(body);return hex.EncodeToString(sum[:])}
   switch mode {
   case "candidate_bytes":candidate=append(candidate,' ')
   case "intent_bytes":intent=append(intent,' ')
   case "source_bytes":docs.Source=[]byte(`{}`)
   case "reference_bytes":docs.Reference=[]byte(`{}`)
   case "mask_bytes":docs.Mask=[]byte(`{}`)
   case "protection_bytes":docs.Protection=[]byte(`{}`)
   case "source_version":p.Source.AssetVersionID=uuid.New()
   case "source_canvas":p.Source.CanvasHash="ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
   case "candidate_state":var value v2input.SemanticCandidate;json.Unmarshal(candidate,&value);value.State="rejected";candidate,_=canonical.Marshal(value);p.Candidate.SHA256=repin(candidate)
   case "intent_kind","snapshot","preserve_persons":
    var value v2input.SemanticEditIntent;json.Unmarshal(intent,&value)
    if mode=="intent_kind" {value.Kind="background_cleanup"};if mode=="snapshot" {value.TargetSnapshotID=uuid.New()};if mode=="preserve_persons" {value.PreservePersons=false}
    intent,_=canonical.Marshal(value);p.Intent.SHA256=repin(intent)
   case "unprotected_mask":var value review.MaskRef;json.Unmarshal(docs.Protection,&value);value.Protected=false;docs.Protection,_=canonical.Marshal(value);p.Protection.SHA256=repin(docs.Protection)
   }
   dependencies,width,height,err:=ResolveRemovalPreviewDocuments(p,candidate,intent,docs)
   if mode=="valid" {
    if err!=nil || width<1 || height<1 || dependencies.Protection!=p.Protection {t.Fatal("valid source closure refused",err)}
   } else if err==nil || width!=0 || height!=0 {t.Fatal("substituted source closure accepted",mode,dependencies,err)}
  })
 }
}
