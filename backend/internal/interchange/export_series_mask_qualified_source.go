package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type ExportSeriesMaskPolicyEvidence struct{Binding v2input.SessionVersionBinding;ModelID,ModelLock,Backend,QualificationSHA string}
type ExportSeriesMaskPolicyAuthority interface{ResolveExportSeriesMaskPolicy(context.Context,ExportSeriesMaskOrigin,v2input.Ref)(ExportSeriesMaskPolicyEvidence,error)}
// Reuses the current imported qualification/model operator authority for person
// masks. Other operations require a dedicated policy and explicitly refuse.
type ExportSeriesPersonMaskPolicy struct{Person ExportStatsPersonPolicyAuthority}
func(s ExportSeriesPersonMaskPolicy)ResolveExportSeriesMaskPolicy(ctx context.Context,origin ExportSeriesMaskOrigin,source v2input.Ref)(ExportSeriesMaskPolicyEvidence,error){
 var zero ExportSeriesMaskPolicyEvidence
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Person==nil||origin.Operation!="mask.person"||origin.Version!="1"{return zero,v2input.ErrInvalid}
 person,err:=s.Person.ResolveExportStatsPersonPolicy(ctx,ExportStatsPersonOrigin{Root:origin.Root.Ref,Params:origin.Params.Ref,Report:origin.Report.Ref,Mask:origin.Mask},source);if err!=nil{return zero,err}
 return ExportSeriesMaskPolicyEvidence{Binding:person.Binding,ModelID:"lraspp_mobilenet_v3_large",ModelLock:person.ModelLock,Backend:person.Backend,QualificationSHA:person.QualificationSHA},ctx.Err()
}

type QualifiedExportSeriesMeasureMaskSource struct{Reader ExportSeriesMaskOriginReader;Policy ExportSeriesMaskPolicyAuthority;Scratch *ExportArchiveScratch;Probe ExportNativeProbe;RuntimeSHA string}
func(q QualifiedExportSeriesMeasureMaskSource)LookupExportSeriesMeasureMask(ctx context.Context,measure ExportSeriesMeasureOrigin,stat v2input.Entry,raw []byte,image ExportClosureNode,ref v2input.Ref)(objectstore.Object,error){
 var zero objectstore.Object
 if err:=ctx.Err();err!=nil{return zero,err}
 if q.Reader.Source==nil||q.Reader.Objects==nil||q.Policy==nil||q.Scratch==nil||q.Probe==nil{return zero,v2input.ErrInvalid}
 if _,err:=exportSeriesMeasureInputRefs(measure,raw,image);err!=nil{return zero,err}
 body:=append([]byte(nil),raw...);imageBody:=append([]byte(nil),image.Body...)
 clone:=func()ExportClosureNode{return ExportClosureNode{Entry:image.Entry,Body:append([]byte(nil),imageBody...),Children:append([]v2input.Entry(nil),image.Children...)}}
 origin,err:=q.Reader.Source.LookupExportSeriesMaskOrigin(ctx,measure,stat,append([]byte(nil),body...),clone(),ref);if err!=nil{return zero,err}
 policy,err:=q.Policy.ResolveExportSeriesMaskPolicy(ctx,origin,image.Entry.Ref);if err!=nil{return zero,err}
 if policy.Binding.Source!=image.Entry.Ref||policy.Binding.AssetVersionID==([16]byte{})||policy.ModelID==""||!validImportRef(v2input.Ref{ArtifactID:ref.ArtifactID,SHA256:policy.ModelLock})||!validImportRef(v2input.Ref{ArtifactID:ref.ArtifactID,SHA256:policy.QualificationSHA})||(policy.Backend!="cpu"&&policy.Backend!="cuda"&&policy.Backend!="mps"){return zero,v2input.ErrInvalid}
 var actual struct{Canvas json.RawMessage `json:"canvas"`};if json.Unmarshal(imageBody,&actual)!=nil{return zero,v2input.ErrInvalid}
 canvas,err:=canonical.Hash(actual.Canvas);if err!=nil||canvas!=policy.Binding.CanvasHash{return zero,v2input.ErrInvalid}
 pinned,bundle,err:=q.Reader.ReadExportSeriesMaskOrigin(ctx,measure,stat,append([]byte(nil),body...),clone(),ref);if err!=nil{return zero,err};if pinned!=origin{return zero,v2input.ErrInvalid}
 var document struct{Report struct{Body struct{Backend string `json:"backend"`} `json:"body"`} `json:"report"`}
 if json.Unmarshal(bundle,&document)!=nil||document.Report.Body.Backend!=policy.Backend{return zero,v2input.ErrInvalid}
 // This model map is derived from current operator qualification, never the
 // archive or report. The native probe still enforces every original claim.
 native,err:=NewNativeExportStyleValidator(q.Scratch,q.Probe,map[string]string{policy.ModelID:policy.ModelLock},q.RuntimeSHA,exportCompositionStagingValidator{});if err!=nil{return zero,err};native.contract="MaskOrigin"
 if _,err:=native.ExportDocumentChildren(ctx,v2input.Entry{Contract:"MaskOrigin",Ref:v2input.Ref{ArtifactID:ref.ArtifactID,SHA256:rawSHA(bundle)}},bundle);err!=nil{return zero,err}
 finalOrigin,finalBundle,err:=q.Reader.ReadExportSeriesMaskOrigin(ctx,measure,stat,append([]byte(nil),body...),clone(),ref);if err!=nil{return zero,err}
 if finalOrigin!=origin||!bytes.Equal(finalBundle,bundle){return zero,v2input.ErrInvalid}
 finalPolicy,err:=q.Policy.ResolveExportSeriesMaskPolicy(ctx,finalOrigin,image.Entry.Ref);if err!=nil{return zero,err};if finalPolicy!=policy{return zero,v2input.ErrInvalid}
 return finalOrigin.Mask,ctx.Err()
}
var _ ExportSeriesMeasureMaskSource=QualifiedExportSeriesMeasureMaskSource{}
var _ ExportSeriesMaskPolicyAuthority=ExportSeriesPersonMaskPolicy{}
