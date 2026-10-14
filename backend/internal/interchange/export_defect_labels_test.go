package interchange

import (
 "context"
 "errors"
 "io"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type exportDefectLabelSourceStub struct{calls int;err error}
func(s *exportDefectLabelSourceStub)ResolveExportDefectLabels(context.Context,v2input.Entry)([]review.VerifiedProposal,error){s.calls++;return nil,s.err}
type exportDefectNoIO struct{t *testing.T}
func(s exportDefectNoIO)Open(context.Context,objectstore.Object)(io.ReadCloser,error){s.t.Fatal("unqualified label map opened");return nil,nil}

func TestDefectLabelQualifierRefusesUnwitnessedOrRevokedBeforeIO(t *testing.T) {
 for _,mode:=range []string{"empty","revoked","contract","missing","cancelled"} {t.Run(mode,func(t *testing.T){
  source:=&exportDefectLabelSourceStub{}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("labels"))},Contract:"DefectLabelMap"}
  object:=objectstore.Object{SHA256:entry.Ref.SHA256,ContentType:"application/octet-stream",Bytes:128,Key:"labels",VersionID:"immutable"}
  qualifier:=ExportDefectLabelQualifier{Objects:exportDefectNoIO{t},Source:source}
  ctx:=context.Background()
  switch mode {case "revoked":source.err=errors.New("revoked");case "contract":entry.Contract="MaskPixels";case "missing":qualifier.Source=nil;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  if err:=qualifier.QualifyExportBinary(ctx,entry,object);err==nil {t.Fatal("unwitnessed labels accepted")}
  expected:=0;if mode=="empty"||mode=="revoked" {expected=1};if source.calls!=expected {t.Fatal("unexpected authority lookup",source.calls)}
 })}
}

func TestDefectLabelRouterRequiresExplicitQualifier(t *testing.T) {
 labels:=&exportBinaryQualifierStub{}
 router:=ExportNativeQualifierRouter{DefectLabels:labels}
 entry:=v2input.Entry{Contract:"DefectLabelMap"}
 if err:=router.QualifyExportBinary(context.Background(),entry,objectstore.Object{});err!=nil||labels.calls!=1 {t.Fatal("label route missing",err)}
 if err:=(ExportNativeQualifierRouter{}).QualifyExportBinary(context.Background(),entry,objectstore.Object{});err==nil {t.Fatal("missing label qualifier accepted")}
}

type exportDefectAuthorityStub struct{calls int}
func(s *exportDefectAuthorityStub)QualifyExportDefectLabel(context.Context,v2input.Entry,[]byte,v2input.Entry)error{s.calls++;return nil}

type exportDefectJSONStub struct{calls int;node ExportClosureNode}
func(s *exportDefectJSONStub)ResolveExportNode(context.Context,v2input.Entry)(ExportClosureNode,error){s.calls++;return s.node,nil}

func TestTypedDefectSourceRequiresBoundAuthorityAndExactParentBeforeFurtherReads(t *testing.T) {
 for _,mode:=range []string{"missing_authority","missing_reader","wrong_contract","parent_hash","parent_identity","no_label_edge"} {t.Run(mode,func(t *testing.T){
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("label"))},Contract:"DefectLabelMap"}
  image:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))}
  raw,err:=canonical.Marshal(map[string]any{"image":image,"label_map":entry.Ref});if err!=nil {t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"DefectCandidates"}
  reader:=&exportDefectJSONStub{node:ExportClosureNode{Entry:parent,Body:raw}}
  authority:=&exportDefectAuthorityStub{}
  source:=ExportTypedDefectLabelSource{Reader:reader,Proposals:[]v2input.Entry{parent},Authority:authority}
  switch mode {case "missing_authority":source.Authority=nil;case "missing_reader":source.Reader=nil;case "wrong_contract":entry.Contract="MaskPixels";case "parent_hash":reader.node.Body=[]byte(`{}`);case "parent_identity":reader.node.Entry.Ref.ArtifactID=uuid.New()}
  if _,err:=source.ResolveExportDefectLabels(context.Background(),entry);err==nil {t.Fatal("invalid proposal source accepted")}
  if authority.calls!=0 {t.Fatal("invalid document reached current authority")}
  expected:=1;if mode=="missing_authority"||mode=="missing_reader"||mode=="wrong_contract" {expected=0}
  if reader.calls!=expected {t.Fatal("unexpected JSON reads",reader.calls)}
 })}
}
