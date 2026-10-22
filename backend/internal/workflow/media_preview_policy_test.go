package workflow

import (
 "encoding/json"
 "strings"
 "testing"
 "photoeditor/backend/internal/media"
)

func requirePreviewDocumentPolicy(t *testing.T,doc RenderDerivativeDocument,policy string) {
 t.Helper();var body struct{Variant string `json:"variant"`;Profile string `json:"profile_hash"`}
 if err:=json.Unmarshal(doc.Params,&body);err!=nil{t.Fatal(err)}
 expected,err:=media.ProfileHashForPolicy(body.Variant,strings.Repeat("e",64),policy)
 if err!=nil || body.Profile!=expected {t.Fatal("preview policy mismatch",body.Profile,expected,err)}
}

func TestRestorationPreviewFrozenLegacyAndCurrentPlans(t *testing.T) {
 _,source,before,after:=restorationPreviewFixture(t)
 old,err:=CompileRestorationPreviewPlan(source,strings.Repeat("e",64),before,after,media.LegacyRendererPolicy);if err!=nil{t.Fatal(err)}
 fresh,err:=CompileRestorationPreviewPlan(source,strings.Repeat("e",64),before,after,media.CurrentRendererPolicy);if err!=nil{t.Fatal(err)}
 replay,err:=CompileRestorationPreviewPlan(source,old.srgb,before,after,old.policy);if err!=nil{t.Fatal(err)}
 if old.run!=fresh.run{t.Fatal("historical identity changed")}
 for i,doc:=range old.docs {
  requirePreviewDocumentPolicy(t,doc.RenderDerivativeDocument,media.LegacyRendererPolicy)
  requirePreviewDocumentPolicy(t,fresh.docs[i].RenderDerivativeDocument,media.CurrentRendererPolicy)
  if doc.ID!=fresh.docs[i].ID || doc.ParamsSHA==fresh.docs[i].ParamsSHA || doc.ParamsSHA!=replay.docs[i].ParamsSHA {t.Fatal("frozen policy replay drift")}
 }
}

func TestPerspectivePreviewFrozenLegacyAndCurrentPlans(t *testing.T) {
 bundle:=perspectivePlanBundle(t)
 old,err:=CompilePerspectivePreviewPlan(bundle,strings.Repeat("e",64),media.LegacyRendererPolicy);if err!=nil{t.Fatal(err)}
 fresh,err:=CompilePerspectivePreviewPlan(bundle,strings.Repeat("e",64),media.CurrentRendererPolicy);if err!=nil{t.Fatal(err)}
 replay,err:=CompilePerspectivePreviewPlan(bundle,old.srgb,old.policy);if err!=nil{t.Fatal(err)}
 if old.run!=fresh.run{t.Fatal("historical identity changed")}
 for i,doc:=range old.documents {
  requirePreviewDocumentPolicy(t,doc,media.LegacyRendererPolicy)
  requirePreviewDocumentPolicy(t,fresh.documents[i],media.CurrentRendererPolicy)
  if doc.ID!=fresh.documents[i].ID || doc.ParamsSHA==fresh.documents[i].ParamsSHA || doc.ParamsSHA!=replay.documents[i].ParamsSHA{t.Fatal("frozen policy replay drift")}
 }
}
