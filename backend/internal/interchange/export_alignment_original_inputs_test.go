package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type alignmentBackgroundStub struct{calls int;deny bool}
func(s *alignmentBackgroundStub)QualifyExportAlignmentBackground(_ context.Context,_ ExportBodyDependencyOrigin,_ v2input.Entry,raw []byte,fit ExportClosureNode,_ v2input.Entry,params []byte,image ExportClosureNode)error{
 s.calls++;raw[0]='!';fit.Body[0]='!';params[0]='!';image.Body[0]='!';if s.deny{return v2input.ErrInvalid};return nil
}
func TestAlignmentOriginalInputsBindBothImagesAndMandatoryMask(t *testing.T){
 for _,mode:=range []string{"valid","mask","mask_missing_authority","mask_denied","source_substitution","changed_image","unknown_field","cancelled"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t)
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  after:=v2input.Entry{Contract:"WorkingImage",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:image.Entry.Ref.SHA256}}
  alignment:=v2input.Entry{Contract:"PairAlignment",Ref:ref()}
  fitRaw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":after.Ref,"alignment":alignment.Ref,"before_pose":ref(),"after_pose":ref(),"before_person":mask,"after_person":mask},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  params:=map[string]any{"before":image.Entry.Ref,"after":after.Ref}
  if mode=="mask"||mode=="mask_missing_authority"||mode=="mask_denied"{params["background_mask"]=mask}
  if mode=="source_substitution"{params["after"]=ref()};if mode=="unknown_field"{params["approved"]=true}
  original,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)}
  origin:=ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Operation:"pair.register",Version:"1"},Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(original)}}
  source:=&fitAlignmentOriginStub{origin:origin}
  nodes:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{image.Entry.Ref.ArtifactID:image,after.Ref.ArtifactID:{Entry:after,Body:append([]byte(nil),image.Body...)}}}
  background:=&alignmentBackgroundStub{deny:mode=="mask_denied"}
  policy:=BoundExportAlignmentOriginalInputs{Source:source,Images:bodyOriginChangingDocuments{base:nodes,change:mode=="changed_image"},Background:background}
  if mode=="mask_missing_authority"{policy.Background=nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=policy.QualifyExportAlignmentOriginalInputs(ctx,origin,parent,raw,fit,alignment,original)
  valid:=mode=="valid"||mode=="mask"
  if (err==nil)!=valid{t.Fatal("unexpected original input qualification",err)}
  if valid&&(source.calls!=2||nodes.calls!=4){t.Fatal("missing final image/origin gate")}
  if mode=="mask"&&background.calls!=1{t.Fatal("mask not qualified")}
  if rawSHA(original)!=origin.Params.SHA256||rawSHA(image.Body)!=image.Entry.Ref.SHA256||rawSHA(fit.Body)!=fit.Entry.Ref.SHA256{t.Fatal("callback mutated evidence")}
 })}
}
