package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "sync"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type preparedMaskWitness struct{parent v2input.Ref;body []byte;contract string;maskBody []byte;child v2input.Entry}
type ImportPreparedMaskWitnesses struct{mu sync.RWMutex;masks map[uuid.UUID]preparedMaskWitness;parents map[uuid.UUID][]preparedMaskWitness;documents map[uuid.UUID]preparedMaskParentDocument;retainedBytes int64;revision uint64}
// Invoked after the complete parent semantic validator succeeds. Retaining a
// witness does not extend SQL authority; geometry rereads its parent/source.
func(w *ImportPreparedMaskWitnesses)remember(parent v2input.Ref,raw []byte,binding v2input.SessionVersionBinding)error{
 if w==nil||!validImportRef(parent)||rawSHA(raw)!=parent.SHA256{return v2input.ErrInvalid}
 mask,err:=review.ParseMaskRef(raw);if err!=nil{return err}
 canvas,err:=canonical.Hash(mask.Canvas);if err!=nil||canvas!=binding.CanvasHash{return v2input.ErrInvalid}
 child:=v2input.Entry{Contract:"MaskPixels",Ref:v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256}}
 if !validImportRef(child.Ref){return v2input.ErrInvalid}
 return w.rememberQualified(preparedMaskWitness{parent:parent,body:append([]byte(nil),raw...),child:child})
}
func(w *ImportPreparedMaskWitnesses)lookup(entry v2input.Entry)(preparedMaskWitness,bool){
 if w==nil{return preparedMaskWitness{},false};w.mu.RLock();defer w.mu.RUnlock()
 value,ok:=w.masks[entry.Ref.ArtifactID];if !ok||value.child!=entry{return preparedMaskWitness{},false};value.body=append([]byte(nil),value.body...);value.maskBody=append([]byte(nil),value.maskBody...);return value,true
}
// Schema/typed references only, used when rereading a geometry witness. Full
// parent semantics are mandatory before remember; this grants no model approval.
type preparedMaskGeometryValidator struct{}
func(preparedMaskGeometryValidator)ImportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if entry.Contract!="MaskRef"||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 mask,err:=review.ParseMaskRef(raw);if err!=nil{return nil,err}
 children:=[]v2input.Entry{{Contract:"MaskPixels",Ref:v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256}}}
 var chain struct{Steps []struct{Inverse *review.ArtifactRef `json:"inverse_field"`} `json:"steps"`}
 if err:=json.Unmarshal(mask.TransformChain,&chain);err!=nil{return nil,err}
 for _,step:=range chain.Steps{if step.Inverse!=nil{children=append(children,v2input.Entry{Contract:"DenseWarpField",Ref:v2input.Ref{ArtifactID:step.Inverse.ID,SHA256:step.Inverse.SHA256}})}}
 return children,ctx.Err()
}
type ImportPreparedMaskGeometry struct{Witnesses *ImportPreparedMaskWitnesses;Catalog ImportDocumentCatalog;Objects ImportDocumentObjects;Masters ImportMasterReader;Binding v2input.SessionVersionBinding}
func(s ImportPreparedMaskGeometry)ResolveExportArrayGeometry(ctx context.Context,entry v2input.Entry)(ExportArrayGeometry,error){
 var zero ExportArrayGeometry
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Catalog==nil||s.Objects==nil||s.Masters==nil||entry.Contract!="MaskPixels"||!validImportRef(s.Binding.Source)||s.Binding.AssetVersionID==uuid.Nil{return zero,v2input.ErrInvalid}
 return resolvePreparedMaskWitnessSet(ctx,s.Witnesses,entry,func(ctx context.Context,w preparedMaskWitness)(ExportArrayGeometry,error){return s.resolveWitnessGeometry(ctx,entry,w)})
}
func(s ImportPreparedMaskGeometry)resolveWitnessGeometry(ctx context.Context,entry v2input.Entry,witness preparedMaskWitness)(ExportArrayGeometry,error){
 var zero ExportArrayGeometry
 if err:=ctx.Err();err!=nil{return zero,err}
 reader:=ImportJSONNodeResolver{Catalog:s.Catalog,Objects:s.Objects,Validator:preparedMaskGeometryValidator{}}
 contract:=witness.contract;if contract==""{contract="MaskRef"}
 if contract=="Override"||contract=="Params"||contract=="DefectProposeReport"{reader.Validator=ImportPreparedSchemaChildren{}}
 parent:=v2input.Entry{Ref:witness.parent,Contract:contract}
 node,err:=reader.ResolveImportNode(ctx,parent);if err!=nil{return zero,err}
 if !bytes.Equal(node.Body,witness.body){return zero,v2input.ErrInvalid}
 maskRaw:=node.Body
 if contract=="Override"||contract=="Params"||contract=="DefectProposeReport"{
  if err:=validateInlinePreparedMaskWitness(node.Body,witness.maskBody,entry);err!=nil{return zero,err}
  maskRaw=witness.maskBody
 }
 mask,err:=review.ParseMaskRef(maskRaw);if err!=nil{return zero,err}
 master,err:=s.Masters.ReadImportMaster(ctx,s.Binding.Source,1<<20);if err!=nil{return zero,err};master=append([]byte(nil),master...)
 if rawSHA(master)!=s.Binding.Source.SHA256{return zero,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(master);if err!=nil{return zero,err}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return zero,err}
 if canvas!=s.Binding.CanvasHash||mask.Canvas!=image.Canvas||image.DType!="float32"{return zero,v2input.ErrInvalid}
 geometry:=ExportArrayGeometry{Width:int(mask.Canvas.Width),Height:int(mask.Canvas.Height),CanvasSHA:canvas,DType:"float32"}
 nativeEntry:=entry;nativeEntry.Contract="MaskArray"
 if _,err:=exportArrayMaximumBytes(nativeEntry,geometry);err!=nil{return zero,err}
 final,err:=reader.ResolveImportNode(ctx,parent);if err!=nil{return zero,err};if !bytes.Equal(final.Body,witness.body){return zero,v2input.ErrInvalid}
 current,err:=s.Masters.ReadImportMaster(ctx,s.Binding.Source,1<<20);if err!=nil{return zero,err};if !bytes.Equal(current,master){return zero,v2input.ErrInvalid}
 return geometry,ctx.Err()
}


// Transport calls mask bytes MaskPixels; the native protocol calls them
// MaskArray. Translation is explicit and retains the identical UUID/SHA.
type importPreparedMaskNativeGeometry struct{Source ImportPreparedMaskGeometry}
func(s importPreparedMaskNativeGeometry)ResolveExportArrayGeometry(ctx context.Context,e v2input.Entry)(ExportArrayGeometry,error){
 if e.Contract!="MaskArray"{return ExportArrayGeometry{},v2input.ErrInvalid};e.Contract="MaskPixels";return s.Source.ResolveExportArrayGeometry(ctx,e)
}
type importPreparedMaskNativeQualifier struct{Native NativeExportArrayQualifier}
func(q importPreparedMaskNativeQualifier)QualifyExportBinary(ctx context.Context,e v2input.Entry,o objectstore.Object)error{
 if e.Contract!="MaskPixels"{return v2input.ErrInvalid};e.Contract="MaskArray";return q.Native.QualifyExportBinary(ctx,e,o)
}
func NewSQLPreparedImportMaskResolver(ctx context.Context,catalog SQLPreparedImportCatalog,objects ImportDocumentObjects,masters ImportMasterReader,witnesses *ImportPreparedMaskWitnesses,scratch *ExportArchiveScratch,probe ExportNativeProbe,runtime string,maxBytes int64)(ImportPreparedBinaryNodeResolver,error){
 var zero ImportPreparedBinaryNodeResolver
 if err:=ctx.Err();err!=nil{return zero,err}
 if catalog.Store.DB==nil||objects==nil||masters==nil||witnesses==nil||scratch==nil||probe==nil||!validImportRefForSHA(runtime)||maxBytes<1||maxBytes>128<<30{return zero,v2input.ErrInvalid}
 catalog.Store.Parsed=append([]byte(nil),catalog.Store.Parsed...)
 source:=ImportPreparedMaskGeometry{Witnesses:witnesses,Catalog:catalog,Objects:objects,Masters:masters,Binding:catalog.Scope.Binding}
 native:=NativeExportArrayQualifier{Objects:objects,Scratch:scratch,Probe:probe,RuntimeSHA:runtime,Geometry:importPreparedMaskNativeGeometry{Source:source}}
 return ImportPreparedBinaryNodeResolver{Catalog:catalog,Objects:objects,Qualifier:importPreparedMaskNativeQualifier{Native:native},MaxBytes:maxBytes},nil
}
