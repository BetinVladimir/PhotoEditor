package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/v2input"
)

type ExportBodyDependencyAuthority interface{QualifyExportBodyDependency(context.Context,v2input.Entry,[]byte,ExportClosureNode)error}
// Must qualify both original pair sources, alignment, poses/person/faces and
// their producer/model/consent ancestry. Fitted report qualification alone is
// insufficient to establish the paired inputs that generated its coefficients.
type ExportBodyFitInputAuthority interface{QualifyExportBodyFitInputs(context.Context,v2input.Entry,[]byte,ExportClosureNode)error}
type BoundExportBodyOriginPolicy struct{
 Source ExportParamsOperationSource
 Images ExportClosureNodeResolver
 Warps ExportDenseWarpOccurrenceAuthority
 Person ExportBodyPersonAuthority
 Dependencies ExportBodyDependencyAuthority
 FitInputs ExportBodyFitInputAuthority
}
func(p BoundExportBodyOriginPolicy)QualifyExportBodyOrigin(ctx context.Context,origin ExportParamsOperationEvidence,parent v2input.Entry,raw,bundleRaw []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if p.Source==nil||p.Images==nil||p.Person==nil||p.Dependencies==nil||len(raw)<2||len(raw)>1<<20||len(bundleRaw)<2||len(bundleRaw)>10<<20||parent.Contract!="Params"||!validImportRef(parent.Ref)||rawSHA(raw)!=parent.Ref.SHA256{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 var bundle exportBodyOriginBundle;if err:=decodeImportDocument(bundleRaw,&bundle);err!=nil{return err}
 if bundle.Schema!=1||bundle.Params.SHA!=parent.Ref.SHA256||!bytes.Equal(bundle.Params.Body,body){return v2input.ErrInvalid}
 var params struct{Image v2input.Ref `json:"image"`;Poses v2input.Ref `json:"poses"`;Fit *v2input.Ref `json:"fit"`;Faces *v2input.Ref `json:"faces"`}
 if json.Unmarshal(body,&params)!=nil||!validImportRef(params.Image)||!validImportRef(params.Poses){return v2input.ErrInvalid}
 current,err:=p.Source.LookupExportParamsOperation(ctx,parent,append([]byte(nil),body...));if err!=nil{return err};if current!=origin||origin.Operation!="geometry.body"||origin.Version!="1"{return v2input.ErrInvalid}
 imageEntry:=v2input.Entry{Ref:params.Image,Contract:"WorkingImage"}
 actual,err:=p.Images.ResolveExportNode(ctx,imageEntry);if err!=nil{return err}
 actualDocument,err:=ownExportCompositionDocument(ctx,actual,imageEntry);if err!=nil{return err}
 if actualDocument.SHA!=bundle.Image.SHA||!bytes.Equal(actualDocument.Body,bundle.Image.Body){return v2input.ErrInvalid}
 image:=ExportClosureNode{Entry:imageEntry,Body:append([]byte(nil),actualDocument.Body...),Children:append([]v2input.Entry(nil),actual.Children...)}
 if err:=qualifyExportResolvedSourceWarps(ctx,image,p.Warps);err!=nil{return err}
 if err:=p.Person.QualifyExportBodyPerson(ctx,parent,append([]byte(nil),body...),ExportClosureNode{Entry:image.Entry,Body:append([]byte(nil),image.Body...),Children:append([]v2input.Entry(nil),image.Children...)});err!=nil{return err}
 qualify:=func(ref v2input.Ref,contract string,document *exportCompositionDocument,fit bool)error{
  if document==nil||document.SHA!=ref.SHA256{return v2input.ErrInvalid}
  node:=ExportClosureNode{Entry:v2input.Entry{Ref:ref,Contract:contract},Body:append([]byte(nil),document.Body...)}
  if _,err:=ownExportCompositionDocument(ctx,node,node.Entry);err!=nil{return err}
  if err:=p.Dependencies.QualifyExportBodyDependency(ctx,parent,append([]byte(nil),body...),node);err!=nil{return err}
  if fit{if p.FitInputs==nil{return v2input.ErrInvalid};return p.FitInputs.QualifyExportBodyFitInputs(ctx,parent,append([]byte(nil),body...),ExportClosureNode{Entry:node.Entry,Body:append([]byte(nil),document.Body...)})}
  return nil
 }
 if err:=qualify(params.Poses,"PoseSet",&bundle.Poses,false);err!=nil{return err}
 if params.Faces!=nil{if err:=qualify(*params.Faces,"FaceLandmarks",bundle.Faces,false);err!=nil{return err}}else if bundle.Faces!=nil{return v2input.ErrInvalid}
 if params.Fit!=nil{if err:=qualify(*params.Fit,"BodyFit",bundle.Fit,true);err!=nil{return err}}else if bundle.Fit!=nil{return v2input.ErrInvalid}
 finalImage,err:=p.Images.ResolveExportNode(ctx,imageEntry);if err!=nil{return err}
 finalDocument,err:=ownExportCompositionDocument(ctx,finalImage,imageEntry);if err!=nil{return err};if !bytes.Equal(finalDocument.Body,actualDocument.Body){return v2input.ErrInvalid}
 if err:=qualifyExportResolvedSourceWarps(ctx,finalImage,p.Warps);err!=nil{return err}
 final,err:=p.Source.LookupExportParamsOperation(ctx,parent,append([]byte(nil),body...));if err!=nil{return err};if final!=origin{return v2input.ErrInvalid};return ctx.Err()
}
var _ ExportBodyOriginPolicy=BoundExportBodyOriginPolicy{}
var _ ExportBodyDependencyAuthority=(*NativeExportBodyDependencyQualifier)(nil)
