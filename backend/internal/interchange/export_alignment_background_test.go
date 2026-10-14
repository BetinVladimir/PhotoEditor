package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type alignmentBackgroundSourceStub struct{object objectstore.Object;calls int;mode string}
func(s *alignmentBackgroundSourceStub)LookupExportAlignmentBackground(_ context.Context,_ ExportBodyDependencyOrigin,_ v2input.Entry,raw []byte,fit ExportClosureNode,_ v2input.Entry,params []byte,image ExportClosureNode)(objectstore.Object,error){
 s.calls++;raw[0]='!';fit.Body[0]='!';params[0]='!';image.Body[0]='!'
 if s.mode=="revoked"&&s.calls>1{return objectstore.Object{},v2input.ErrInvalid}
 out:=s.object;if s.mode=="changed"&&s.calls>1{out.VersionID="changed"};return out,nil
}
func TestAlignmentBackgroundPhysicalQualifierRechecksOriginalSource(t *testing.T){
 for _,mode:=range []string{"valid","changed","revoked","probe","bytes","source_substitution","cancelled"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t)
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  after:=ref();alignment:=v2input.Entry{Contract:"PairAlignment",Ref:ref()}
  fitRaw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":after,"alignment":alignment.Ref,"before_pose":ref(),"after_pose":ref(),"before_person":mask,"after_person":mask},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  before:=image.Entry.Ref;if mode=="source_substitution"{before=ref()}
  params,err:=canonical.Marshal(map[string]any{"before":before,"after":after,"background_mask":mask});if err!=nil{t.Fatal(err)}
  origin:=ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Operation:"pair.register",Version:"1"},Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)}}
  source:=&alignmentBackgroundSourceStub{mode:mode,object:objectstore.Object{Key:"mask",VersionID:"immutable",SHA256:mask.Artifact.SHA256,Bytes:4,ContentType:"application/x-npy"}}
  root:=t.TempDir();scratch:=&ExportArchiveScratch{root:root,maximum:1<<20};runtime:=rawSHA([]byte("runtime"));probe:=&exportMaskProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  data:=[]byte("mask");if mode=="bytes"{data=[]byte("oops")}
  qualifier:=NativeExportAlignmentBackgroundQualifier{Source:source,Objects:statsParamsObjects{data},Scratch:scratch,Probe:probe,RuntimeSHA:runtime}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=qualifier.QualifyExportAlignmentBackground(ctx,origin,parent,raw,fit,alignment,params,image)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected physical mask qualification",err)}
  if mode=="valid"&&source.calls!=2{t.Fatal("missing final source authority")}
  if (mode=="source_substitution"||mode=="cancelled")&&source.calls!=0{t.Fatal("invalid scope reached IO")}
  if rawSHA(params)!=origin.Params.SHA256||rawSHA(image.Body)!=image.Entry.Ref.SHA256||rawSHA(fit.Body)!=fit.Entry.Ref.SHA256{t.Fatal("callback mutated evidence")}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("staging leaked",err)}
 })}
}
