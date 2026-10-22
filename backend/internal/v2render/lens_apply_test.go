package v2render

import (
 "context"
 "encoding/json"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestLensApplyDecisionRequiresExactShownResultAndGeometryAcknowledgement(t *testing.T){
 decision:=LensApplyDecision{SchemaVersion:1,ResultID:uuid.New(),Root:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},WorkingImage:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)},ConfirmGeometryReset:true,AcceptUncoveredArea:true}
 raw,_:=canonical.Marshal(decision)
 if _,err:=DecodeLensApplyDecision(raw);err!=nil{t.Fatal(err)}
 for _,change:=range []func(map[string]any){func(v map[string]any){delete(v,"confirm_geometry_reset")},func(v map[string]any){v["accept_uncovered_area"]=nil},func(v map[string]any){v["untrusted"]=true},func(v map[string]any){v["result_id"]=uuid.Nil.String()},func(v map[string]any){v["working_image"]=map[string]any{"artifact_id":uuid.NewString(),"sha256":"bad"}}}{
  var value map[string]any;_ = json.Unmarshal(raw,&value);change(value);changed,_:=json.Marshal(value)
  if _,err:=DecodeLensApplyDecision(changed);err==nil{t.Fatal("invalid decision accepted",string(changed))}
 }
}
func TestLensApplyCancelledBeforeAuthority(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(LensProposalService{}).Apply(ctx,auth.Identity{},uuid.New(),uuid.New(),uuid.New(),uuid.New(),LensApplyDecision{});err!=context.Canceled{t.Fatal(err)}
}
