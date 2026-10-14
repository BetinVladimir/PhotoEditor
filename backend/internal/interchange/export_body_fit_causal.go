package interchange

import (
 "bytes"
 "context"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type ExportBodyFitAlignmentAuthority interface {
 QualifyExportBodyFitAlignment(context.Context,v2input.Entry,[]byte,ExportClosureNode,ExportClosureNode)error
}
type ExportBodyFitFeatureAuthority interface {
 QualifyExportBodyFitFeature(context.Context,v2input.Entry,[]byte,ExportClosureNode,ExportClosureNode)error
}
// This authority must qualify both actual source images and both physical person
// masks under the original fit producer, including current consent/model policy.
// A schema-only implementation does not satisfy this port.
type ExportBodyFitPairedPersonAuthority interface {
 QualifyExportBodyFitPairedPersons(context.Context,v2input.Entry,[]byte,ExportClosureNode)error
}

// Composes independent original-producer authorities. Re-reading the complete
// pinned bundle brackets callbacks so no callback can replace an accepted edge.
type BoundExportBodyFitCausal struct {
 Reader ExportBodyFitBundleReader
 Documents ExportClosureNodeResolver
 Alignment ExportBodyFitAlignmentAuthority
 Features ExportBodyFitFeatureAuthority
 Persons ExportBodyFitPairedPersonAuthority
}
func(q BoundExportBodyFitCausal)QualifyExportBodyFitCausalInputs(ctx context.Context,parent v2input.Entry,raw []byte,fit ExportClosureNode,bundle []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.Reader==nil||q.Documents==nil||q.Alignment==nil||q.Features==nil||q.Persons==nil||parent.Contract!="Params"||!validImportRef(parent.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=parent.Ref.SHA256||len(bundle)<2||len(bundle)>16<<20{return v2input.ErrInvalid}
 raw=append([]byte(nil),raw...);bundle=append([]byte(nil),bundle...)
 fit=ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}
 if _,err:=exportBodyFitInputInventory(fit);err!=nil{return err}
 clone:=func()ExportClosureNode{return ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}}
 current:=func()error{
  actual,err:=q.Reader.ReadExportBodyFitInputs(ctx,parent,append([]byte(nil),raw...),clone());if err!=nil{return err}
  if !bytes.Equal(actual,bundle){return v2input.ErrInvalid};return ctx.Err()
 }
 if err:=current();err!=nil{return err}
 refs,err:=v2draft.DocumentChildren(fit.Body,"BodyFit",fit.Entry.Ref);if err!=nil{return err}
 for _,ref:=range refs{
  alignment:=ref.Path=="Document.params.alignment"
  feature:=ref.Path=="Document.params.before_pose"||ref.Path=="Document.params.after_pose"||ref.Path=="Document.params.before_faces"||ref.Path=="Document.params.after_faces"
  if !alignment&&!feature{continue}
  expected:=v2input.Entry{Contract:ref.Contract,Ref:ref.Ref}
  node,err:=q.Documents.ResolveExportNode(ctx,expected);if err!=nil{return err}
  doc,err:=ownExportCompositionDocument(ctx,node,expected);if err!=nil{return err}
  if len(doc.Body)>2<<20{return v2input.ErrInvalid}
  child:=ExportClosureNode{Entry:expected,Body:append([]byte(nil),doc.Body...)}
  if alignment{err=q.Alignment.QualifyExportBodyFitAlignment(ctx,parent,append([]byte(nil),raw...),clone(),child)}else{err=q.Features.QualifyExportBodyFitFeature(ctx,parent,append([]byte(nil),raw...),clone(),child)}
  if err!=nil{return err}
  if err:=current();err!=nil{return err}
 }
 if err:=q.Persons.QualifyExportBodyFitPairedPersons(ctx,parent,append([]byte(nil),raw...),clone());err!=nil{return err}
 return current()
}
var _ ExportBodyFitCausalAuthority=BoundExportBodyFitCausal{}
