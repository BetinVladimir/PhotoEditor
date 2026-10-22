package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// Source must bind archived report/proposal/label objects to this accepted
// entry; a bare PNG SHA or an untrusted report does not grant copy authority.
type ImportDefectPNGOccurrencePublisher interface{
 PublishImportDefectPNG(context.Context,uuid.UUID,v2input.Ref,objectstore.Object,v2input.Ref,[]v2input.Ref)(v2input.Ref,error)
}
type ImportDefectPNGBinaryRemapper struct{
 Workflow,Entry uuid.UUID
 Old,Target v2input.SessionVersionBinding
 Sources ImportRecipeSourceQualifier
 Catalog ImportDocumentCatalog
 Objects ImportDocumentObjects
 Source ExportDefectPNGSource
 Publisher ImportDefectPNGOccurrencePublisher
}
func(r ImportDefectPNGBinaryRemapper)RemapImportBinary(ctx context.Context,entry uuid.UUID,archived,staged v2input.Ref,kind string,old,target v2input.SessionVersionBinding,maxBytes int64)(v2input.Ref,int64,error){
 zero:=v2input.Ref{}
 if err:=ctx.Err();err!=nil{return zero,0,err}
 if kind!="DefectLabelPNG"{return zero,0,&ImportRecipeUnsupported{Reason:"binary_dependency_codec_not_installed"}}
 if r.Publisher==nil{return zero,0,&ImportRecipeUnsupported{Reason:"defect_png_occurrence_publisher_not_installed"}}
 if r.Workflow==uuid.Nil||r.Entry==uuid.Nil||entry!=r.Entry||old!=r.Old||target!=r.Target||r.Sources==nil||r.Catalog==nil||r.Objects==nil||r.Source==nil||!validImportRef(archived)||!validImportRef(staged)||staged.SHA256!=archived.SHA256||!validImportRef(old.Source)||!validImportRef(target.Source)||old.AssetVersionID==uuid.Nil||target.AssetVersionID==uuid.Nil||old.AssetVersionID==target.AssetVersionID||old.CanvasHash!=target.CanvasHash||maxBytes<1||maxBytes>128<<30{return zero,0,v2input.ErrInvalid}
 if err:=r.Sources.QualifyImportRecipeSource(ctx,entry,old,target);err!=nil{return zero,0,err}
 original:=v2input.Entry{Ref:archived,Contract:kind}
 physical:=ExportDefectPNGQualifier{Objects:r.Objects,Source:r.Source}
 stagedEntry:=v2input.Entry{Ref:staged,Contract:"Artifact"}
 object,err:=r.Catalog.LookupImportDocument(ctx,stagedEntry);if err!=nil{return zero,0,err}
 if object.Bytes<1||object.Bytes>maxBytes||object.SHA256!=archived.SHA256||object.ContentType!="image/png"{return zero,0,v2input.ErrInvalid}
 if err:=physical.QualifyExportBinary(ctx,original,object);err!=nil{return zero,0,err}
 inputs,err:=r.Source.ResolveExportDefectPNG(ctx,original);if err!=nil{return zero,0,err}
 if _,err:=exportDefectPNGBindings(inputs);err!=nil{return zero,0,err}
 reports:=make([]v2input.Ref,len(inputs));for i,input:=range inputs{reports[i]=input.Report}
 current,err:=r.Catalog.LookupImportDocument(ctx,stagedEntry);if err!=nil{return zero,0,err};if current!=object{return zero,0,objectstore.ErrIntegrity}
 if err:=r.Sources.QualifyImportRecipeSource(ctx,entry,old,target);err!=nil{return zero,0,err}
 mapped:=v2input.Ref{ArtifactID:uuid.NewSHA1(r.Workflow,[]byte("interchange:dependency:"+entry.String()+":DefectLabelPNG:"+archived.ArtifactID.String())),SHA256:archived.SHA256}
 if mapped.ArtifactID==archived.ArtifactID{return zero,0,v2input.ErrInvalid}
 receipt,err:=r.Publisher.PublishImportDefectPNG(ctx,entry,mapped,object,archived,reports);if err!=nil{return zero,0,err};if receipt!=mapped{return zero,0,objectstore.ErrIntegrity}
 // Publication does not exempt the source from revocation or object changes.
 current,err=r.Catalog.LookupImportDocument(ctx,stagedEntry);if err!=nil{return zero,0,err};if current!=object{return zero,0,objectstore.ErrIntegrity}
 if err:=physical.QualifyExportBinary(ctx,original,object);err!=nil{return zero,0,err}
 if err:=r.Sources.QualifyImportRecipeSource(ctx,entry,old,target);err!=nil{return zero,0,err}
 return mapped,object.Bytes,ctx.Err()
}
var _ ImportBinaryRemapper=ImportDefectPNGBinaryRemapper{}

// Constructed by the workflow factory; entry selection cannot choose another
// entry's source witnesses or publication identity.
type importEntryDefectPNGRouter struct{entries map[uuid.UUID]ImportDefectPNGBinaryRemapper}
func(r importEntryDefectPNGRouter)RemapImportBinary(ctx context.Context,entry uuid.UUID,archived,staged v2input.Ref,kind string,old,target v2input.SessionVersionBinding,maxBytes int64)(v2input.Ref,int64,error){
 if err:=ctx.Err();err!=nil{return v2input.Ref{},0,err}
 selected,ok:=r.entries[entry];if !ok{return v2input.Ref{},0,v2input.ErrInvalid}
 return selected.RemapImportBinary(ctx,entry,archived,staged,kind,old,target,maxBytes)
}
