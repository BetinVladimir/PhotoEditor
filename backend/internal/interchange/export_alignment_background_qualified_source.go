package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type QualifiedExportAlignmentBackgroundSource struct{Reader ExportAlignmentBackgroundOriginReader;Policy ExportSeriesMaskPolicyAuthority;Scratch *ExportArchiveScratch;Probe ExportNativeProbe;RuntimeSHA string}
func(q QualifiedExportAlignmentBackgroundSource)LookupExportAlignmentBackground(ctx context.Context,registration ExportBodyDependencyOrigin,parent v2input.Entry,raw []byte,fit ExportClosureNode,alignment v2input.Entry,params []byte,image ExportClosureNode)(objectstore.Object,error){
 var zero objectstore.Object
 if err:=ctx.Err();err!=nil{return zero,err}
 if q.Reader.Source==nil||q.Reader.Objects==nil||q.Policy==nil||q.Scratch==nil||q.Probe==nil{return zero,v2input.ErrInvalid}
 mask,err:=exportAlignmentBackgroundProjection(registration,parent,raw,fit,alignment,params,image);if err!=nil{return zero,err}
 ref:=v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256}
 original:=append([]byte(nil),params...);fitBody:=append([]byte(nil),fit.Body...)
 fitClone:=func()ExportClosureNode{return ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fitBody...)}}
 body:=append([]byte(nil),raw...);imageBody:=append([]byte(nil),image.Body...)
 clone:=func()ExportClosureNode{return ExportClosureNode{Entry:image.Entry,Body:append([]byte(nil),imageBody...),Children:append([]v2input.Entry(nil),image.Children...)}}
 origin,err:=q.Reader.Source.LookupExportAlignmentBackgroundOrigin(ctx,registration,parent,append([]byte(nil),body...),fitClone(),alignment,append([]byte(nil),original...),clone());if err!=nil{return zero,err}
 policy,err:=q.Policy.ResolveExportSeriesMaskPolicy(ctx,origin,image.Entry.Ref);if err!=nil{return zero,err}
 if policy.Binding.Source!=image.Entry.Ref||policy.Binding.AssetVersionID==([16]byte{})||policy.ModelID==""||!validImportRef(v2input.Ref{ArtifactID:ref.ArtifactID,SHA256:policy.ModelLock})||!validImportRef(v2input.Ref{ArtifactID:ref.ArtifactID,SHA256:policy.QualificationSHA})||(policy.Backend!="cpu"&&policy.Backend!="cuda"&&policy.Backend!="mps"){return zero,v2input.ErrInvalid}
 var actual struct{Canvas json.RawMessage `json:"canvas"`};if json.Unmarshal(imageBody,&actual)!=nil{return zero,v2input.ErrInvalid}
 canvas,err:=canonical.Hash(actual.Canvas);if err!=nil||canvas!=policy.Binding.CanvasHash{return zero,v2input.ErrInvalid}
 pinned,bundle,err:=q.Reader.ReadExportAlignmentBackgroundOrigin(ctx,registration,parent,append([]byte(nil),body...),fitClone(),alignment,append([]byte(nil),original...),clone());if err!=nil{return zero,err};if pinned!=origin{return zero,v2input.ErrInvalid}
 var document struct{Report struct{Body struct{Backend string `json:"backend"`} `json:"body"`} `json:"report"`}
 if json.Unmarshal(bundle,&document)!=nil||document.Report.Body.Backend!=policy.Backend{return zero,v2input.ErrInvalid}
 // This model map is derived from current operator qualification, never the
 // archive or report. The native probe still enforces every original claim.
 native,err:=NewNativeExportStyleValidator(q.Scratch,q.Probe,map[string]string{policy.ModelID:policy.ModelLock},q.RuntimeSHA,exportCompositionStagingValidator{});if err!=nil{return zero,err};native.contract="MaskOrigin"
 if _,err:=native.ExportDocumentChildren(ctx,v2input.Entry{Contract:"MaskOrigin",Ref:v2input.Ref{ArtifactID:ref.ArtifactID,SHA256:rawSHA(bundle)}},bundle);err!=nil{return zero,err}
 finalOrigin,finalBundle,err:=q.Reader.ReadExportAlignmentBackgroundOrigin(ctx,registration,parent,append([]byte(nil),body...),fitClone(),alignment,append([]byte(nil),original...),clone());if err!=nil{return zero,err}
 if finalOrigin!=origin||!bytes.Equal(finalBundle,bundle){return zero,v2input.ErrInvalid}
 finalPolicy,err:=q.Policy.ResolveExportSeriesMaskPolicy(ctx,finalOrigin,image.Entry.Ref);if err!=nil{return zero,err};if finalPolicy!=policy{return zero,v2input.ErrInvalid}
 return finalOrigin.Mask,ctx.Err()
}
var _ ExportAlignmentBackgroundSource=QualifiedExportAlignmentBackgroundSource{}
