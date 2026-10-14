package interchange

import (
 "context"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type ExportRenderFrameAuthority interface{
 QualifyExportRenderFrame(context.Context,v2draft.LayerRef,[]byte)error
}
type ExportDescriptorWorkingAuthority struct{
 Parents,Descriptors ExportClosureNodeResolver
 Recipes []exportRecipeBinding
 Authority ExportRenderFrameAuthority
}
func(s ExportDescriptorWorkingAuthority)QualifyAdditionalExportWorking(ctx context.Context,entry v2input.Entry,raw []byte)(string,error){
 if entry.Contract!="WorkingImage"{return "",v2input.ErrInvalid}
 return s.qualifyDescriptorFrame(ctx,entry,raw,nil)
}
func(s ExportDescriptorWorkingAuthority)qualifyDescriptorFrame(ctx context.Context,entry v2input.Entry,raw []byte,array *v2input.Entry)(string,error){
 if err:=ctx.Err();err!=nil{return "",err}
 if s.Parents==nil||s.Descriptors==nil||s.Authority==nil||(entry.Contract!="WorkingImage"&&entry.Contract!="FaceObservations")||!validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256{return "",v2input.ErrInvalid}
 matched:=false;canvas:=""
 for _,expected:=range s.Recipes{
  parent:=v2input.Entry{Ref:expected.ref,Contract:"FrozenRenderRecipe"}
  node,err:=s.Parents.ResolveExportNode(ctx,parent);if err!=nil{return "",err}
  if node.Entry!=parent||rawSHA(node.Body)!=parent.Ref.SHA256{return "",v2input.ErrInvalid}
  frozen,err:=v2draft.DecodeFrozenRenderRecipe(node.Body);if err!=nil||frozen.Scalars.Source!=expected.binding{return "",v2input.ErrInvalid}
  dependencies,err:=v2draft.FrozenRenderRecipeDependencies(node.Body,parent.Ref);if err!=nil{return "",err}
  for _,dependency:=range dependencies{
   if dependency.Contract!="DescriptorShard"{continue}
   descriptor:=v2input.Entry{Ref:dependency.Ref,Contract:"DescriptorShard"}
   edge:=false;for _,child:=range node.Children{if child.Ref.ArtifactID==descriptor.Ref.ArtifactID{if child!=descriptor{return "",v2input.ErrInvalid};edge=true}}
   if !edge{return "",v2input.ErrInvalid}
   shard,err:=s.Descriptors.ResolveExportNode(ctx,descriptor);if err!=nil{return "",err}
   if shard.Entry!=descriptor||rawSHA(shard.Body)!=descriptor.Ref.SHA256{return "",v2input.ErrInvalid}
   children,err:=v2draft.DocumentOccurrenceChildren(shard.Body,dependency);if err!=nil{return "",err}
   for _,child:=range children{
    if child.Contract!=entry.Contract||child.Ref.ArtifactID!=entry.Ref.ArtifactID{continue}
    if child.Ref!=entry.Ref||child.ExpectedBinding==nil||child.RenderFrame==nil||child.ExpectedBinding.CanvasHash==""{return "",v2input.ErrInvalid}
    found:=false;for _,actual:=range shard.Children{if actual.Ref.ArtifactID==entry.Ref.ArtifactID{if actual!=entry{return "",v2input.ErrInvalid};found=true}}
    if !found{return "",v2input.ErrInvalid}
    descendants,err:=v2draft.DocumentOccurrenceChildren(raw,child);if err!=nil{return "",err}
    if array!=nil{
     arrayAuthority,ok:=s.Authority.(ExportRenderFrameArrayAuthority);if !ok{return "",v2input.ErrInvalid}
     arrayFound:=false
     for _,binary:=range descendants{if binary.Ref.ArtifactID==array.Ref.ArtifactID{
      if binary.Ref!=array.Ref||binary.Contract!=array.Contract||binary.RenderFrame==nil||binary.ExpectedBinding==nil{return "",v2input.ErrInvalid}
      if err:=arrayAuthority.QualifyExportRenderFrameArray(ctx,binary);err!=nil{return "",err};arrayFound=true
     }}
     if !arrayFound{return "",v2input.ErrInvalid}
    }
    if err:=s.Authority.QualifyExportRenderFrame(ctx,child,append([]byte(nil),raw...));err!=nil{return "",err}
    if matched&&canvas!=child.ExpectedBinding.CanvasHash{return "",v2input.ErrInvalid};canvas=child.ExpectedBinding.CanvasHash;matched=true
   }
  }
 }
 if !matched{if array!=nil{return "",ctx.Err()};return "",v2input.ErrInvalid};return canvas,ctx.Err()
}
func(s SQLExportDocumentCatalog)QualifyExportRenderFrame(ctx context.Context,node v2draft.LayerRef,raw []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if (node.Contract!="WorkingImage"&&node.Contract!="FaceObservations")||node.RenderFrame==nil||node.ExpectedBinding==nil||!validImportRef(node.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=node.Ref.SHA256{return v2input.ErrInvalid}
 if _,err:=v2draft.DocumentOccurrenceChildren(raw,node);err!=nil{return err}
 current,err:=s.LookupExportDocument(ctx,v2input.Entry{Ref:node.Ref,Contract:node.Contract});if err!=nil{return err}
 catalog:=v2draft.SQLArtifactCatalog{DB:s.DB,Bucket:s.Bucket,ProjectID:s.Request.ProjectID,AssetVersionID:node.RenderFrame.SelectedVersion}
 object,err:=catalog.Lookup(ctx,s.Who,s.Tenant,node);if err!=nil{return err}
 if object!=current||object.Bytes!=int64(len(raw)){return v2input.ErrInvalid};return ctx.Err()
}
var _ ExportAdditionalWorkingAuthority=ExportDescriptorWorkingAuthority{}
var _ ExportRenderFrameAuthority=SQLExportDocumentCatalog{}


// Face documents retain descriptor occurrence context; generic refs cannot
// independently establish the analysis/ordinal/model authority for this node.
type ExportDescriptorFaceValidator struct{Frames ExportDescriptorWorkingAuthority;Other ExportTypedDocumentValidator}
func(s ExportDescriptorFaceValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if entry.Contract!="FaceObservations"{if s.Other==nil{return nil,v2input.ErrInvalid};return s.Other.ExportDocumentChildren(ctx,entry,raw)}
 if _,err:=s.Frames.qualifyDescriptorFrame(ctx,entry,raw,nil);err!=nil{return nil,err}
 refs,err:=v2draft.DocumentChildren(raw,entry.Contract,entry.Ref);if err!=nil{return nil,err}
 children:=make([]v2input.Entry,0,len(refs));for _,ref:=range refs{children=append(children,v2input.Entry{Ref:ref.Ref,Contract:ref.Contract})}
 return children,ctx.Err()
}
