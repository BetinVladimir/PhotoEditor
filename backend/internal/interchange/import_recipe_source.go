package interchange

import (
 "context"
 "bytes"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type importArchivedSource struct{staged v2input.Ref;sha,contract string}
// ParsedImportRecipeSourceQualifier does not trust a recipe's original UUIDs:
// they resolve only through the immutable bundle inventory into staged files.
// The current master reader separately authorizes the post-ingest SQL source.
type ParsedImportRecipeSourceQualifier struct{
 originals map[uuid.UUID]string
 archive map[uuid.UUID]importArchivedSource
 reader ImportArchiveDocumentReader
 masters ImportMasterReader
}
func NewParsedImportRecipeSourceQualifier(parsedRaw []byte,reader ImportArchiveDocumentReader,masters ImportMasterReader)(*ParsedImportRecipeSourceQualifier,error){
 if reader==nil||masters==nil{return nil,v2input.ErrInvalid}
 if _,err:=validateParsedImportDocument(context.Background(),parsedRaw);err!=nil{return nil,err}
 var parsed importParsedDocument;if err:=decodeImportDocument(parsedRaw,&parsed);err!=nil{return nil,err}
 files:=map[string]v2input.Ref{};for _,file:=range parsed.Files{files[file.Path]=file.Content}
 originals:=map[uuid.UUID]string{};for _,entry:=range parsed.Manifest.Entries{originals[entry.ID]=entry.ImageSHA}
 archive:=map[uuid.UUID]importArchivedSource{}
 for _,item:=range parsed.Manifest.Inventory{
  if _,exists:=archive[item.ID];exists{return nil,v2input.ErrInvalid}
  archive[item.ID]=importArchivedSource{staged:files[item.Path],sha:item.SHA,contract:item.Contract}
 }
 return &ParsedImportRecipeSourceQualifier{originals:originals,archive:archive,reader:reader,masters:masters},nil
}
func(q *ParsedImportRecipeSourceQualifier)QualifyImportRecipeSource(ctx context.Context,entry uuid.UUID,old,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if q==nil||q.reader==nil||q.masters==nil||old.AssetVersionID==uuid.Nil||target.AssetVersionID==uuid.Nil||old.AssetVersionID==target.AssetVersionID||old.Source.ArtifactID==target.Source.ArtifactID||!validImportRef(old.Source)||!validImportRef(target.Source){return v2input.ErrInvalid}
 original,ok:=q.originals[entry];if !ok{return v2input.ErrInvalid}
 archived,ok:=q.archive[old.Source.ArtifactID]
 if !ok||archived.contract!="WorkingImage"||archived.sha!=old.Source.SHA256{return v2input.ErrInvalid}
 oldRaw,err:=q.reader.ReadImportArchiveDocument(ctx,archived.staged,1<<20);if err!=nil{return err}
 oldRaw=append([]byte(nil),oldRaw...)
 if len(oldRaw)<2||len(oldRaw)>1<<20||rawSHA(oldRaw)!=old.Source.SHA256{return v2input.ErrInvalid}
 var oldTyped review.WorkingImage;if err:=decodeImportDocument(oldRaw,&oldTyped);err!=nil{return err}
 oldImage,err:=review.ParseWorkingImage(oldRaw);if err!=nil{return err}
 oldCanvas,err:=canonical.Hash(oldImage.Canvas);if err!=nil{return err}
 if oldCanvas!=old.CanvasHash||oldImage.Canvas.SourceSHA256!=original{return v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return err}
 newRaw,err:=q.masters.ReadImportMaster(ctx,target.Source,1<<20);if err!=nil{return err}
 newRaw=append([]byte(nil),newRaw...)
 if len(newRaw)<2||len(newRaw)>1<<20||rawSHA(newRaw)!=target.Source.SHA256{return v2input.ErrInvalid}
 var newTyped review.WorkingImage;if err:=decodeImportDocument(newRaw,&newTyped);err!=nil{return err}
 newImage,err:=review.ParseWorkingImage(newRaw);if err!=nil{return err}
 newCanvas,err:=canonical.Hash(newImage.Canvas);if err!=nil{return err}
 if newCanvas!=target.CanvasHash||newImage.Canvas.SourceSHA256!=original||newImage.DType!="float32"||newImage.Color.ProfileID!="work_rec2020_gamma24_v1"||newImage.Color.Encoding!="gamma24"{return v2input.ErrInvalid}
 // No inferred scale/rotation: portable masks and authored geometry require
 // the exact canonical canvas. Different geometry needs a separate remapper.
 if oldCanvas!=newCanvas{return &ImportRecipeUnsupported{Reason:"source_canvas_remap_required"}}
 if oldImage.DType!="float32"||oldImage.Color.ProfileID!="work_rec2020_gamma24_v1"||oldImage.Color.Encoding!="gamma24"{return &ImportRecipeUnsupported{Reason:"source_color_remap_required"}}
 // Each reader repeats its own current SQL/object lookup. Repeat both after
 // all semantic IO; neither original nor canonical source authority may be
 // retained from before reading the other source.
 if err:=ctx.Err();err!=nil{return err}
 currentOld,err:=q.reader.ReadImportArchiveDocument(ctx,archived.staged,1<<20);if err!=nil{return err}
 if !bytes.Equal(currentOld,oldRaw){return v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return err}
 currentNew,err:=q.masters.ReadImportMaster(ctx,target.Source,1<<20);if err!=nil{return err}
 if !bytes.Equal(currentNew,newRaw){return v2input.ErrInvalid}
 return ctx.Err()
}
