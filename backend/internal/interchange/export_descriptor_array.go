package interchange

import (
 "context"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type ExportRenderFrameArrayAuthority interface{QualifyExportRenderFrameArray(context.Context,v2draft.LayerRef)error}
type ExportWorkingArrayOccurrenceAuthority interface{QualifyExportWorkingArrayOccurrence(context.Context,v2input.Entry,[]byte,v2input.Entry)error}
func(s SQLExportDocumentCatalog)QualifyExportRenderFrameArray(ctx context.Context,node v2draft.LayerRef)error{
 if err:=ctx.Err();err!=nil{return err}
 if (node.Contract!="WorkingPixels"&&node.Contract!="WorkingAlpha")||node.RenderFrame==nil||node.ExpectedBinding==nil||!validImportRef(node.Ref)||node.ParentRef!=node.ExpectedBinding.Source{return v2input.ErrInvalid}
 current,err:=s.LookupExportDocument(ctx,v2input.Entry{Ref:node.Ref,Contract:node.Contract});if err!=nil{return err}
 catalog:=v2draft.SQLArtifactCatalog{DB:s.DB,Bucket:s.Bucket,ProjectID:s.Request.ProjectID,AssetVersionID:node.RenderFrame.SelectedVersion}
 object,err:=catalog.Lookup(ctx,s.Who,s.Tenant,node);if err!=nil{return err}
 if object!=current{return v2input.ErrInvalid};return ctx.Err()
}
func(s ExportDescriptorFaceValidator)QualifyExportWorkingArrayOccurrence(ctx context.Context,parent v2input.Entry,raw []byte,array v2input.Entry)error{
 if err:=ctx.Err();err!=nil{return err}
 if parent.Contract!="WorkingImage"||(array.Contract!="WorkingPixels"&&array.Contract!="WorkingAlpha"){return v2input.ErrInvalid}
 _,err:=s.Frames.qualifyDescriptorFrame(ctx,parent,raw,&array);return err
}
func(s *ExportFrozenCompositionValidator)QualifyExportWorkingArrayOccurrence(ctx context.Context,parent v2input.Entry,raw []byte,array v2input.Entry)error{
 if err:=ctx.Err();err!=nil{return err}
 if s==nil{return v2input.ErrInvalid}
 authority,ok:=s.base.(ExportWorkingArrayOccurrenceAuthority);if !ok{return v2input.ErrInvalid}
 return authority.QualifyExportWorkingArrayOccurrence(ctx,parent,raw,array)
}
func(s ExportJSONNodeResolver)QualifyExportWorkingArrayOccurrence(ctx context.Context,parent v2input.Entry,raw []byte,array v2input.Entry)error{
 if err:=ctx.Err();err!=nil{return err}
 authority,ok:=s.Validator.(ExportWorkingArrayOccurrenceAuthority);if !ok{return v2input.ErrInvalid}
 return authority.QualifyExportWorkingArrayOccurrence(ctx,parent,raw,array)
}
var _ ExportRenderFrameArrayAuthority=SQLExportDocumentCatalog{}
