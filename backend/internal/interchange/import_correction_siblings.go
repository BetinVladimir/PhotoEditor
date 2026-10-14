package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type importArchivedCorrectionRoot struct{Ref v2input.Ref;Binding v2input.SessionVersionBinding}

func extendImportCorrectionBindings(ctx context.Context,sources ImportSourceInventory,parsed importParsedDocument,q *ParsedImportRecipeSourceQualifier,bindings []ImportCorrectionBinding,plans []importArchivedCorrectionRoot)([]ImportCorrectionBinding,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if q==nil||q.reader==nil||q.masters==nil||len(bindings)>5000||len(plans)>5000{return nil,v2input.ErrInvalid}
 result:=append([]ImportCorrectionBinding(nil),bindings...)
 byVersion:=map[uuid.UUID]ImportCorrectionBinding{};byEntry:=map[uuid.UUID]ImportCorrectionBinding{}
 for _,item:=range result{
  if _,ok:=byVersion[item.Old.AssetVersionID];ok{return nil,v2input.ErrInvalid}
  if _,ok:=byEntry[item.Entry];ok{return nil,v2input.ErrInvalid}
  byVersion[item.Old.AssetVersionID]=item;byEntry[item.Entry]=item
 }
 type planPin struct{ref v2input.Ref;size int}
 pins:=[]planPin{}
 consumed:=0
 for _,plan:=range plans{
  if err:=ctx.Err();err!=nil{return nil,err}
  archived,ok:=q.archive[plan.Ref.ArtifactID]
  if !ok||archived.contract!="CorrectionPlan"||archived.sha!=plan.Ref.SHA256{return nil,v2input.ErrInvalid}
  raw,err:=q.reader.ReadImportArchiveDocument(ctx,archived.staged,8<<20);if err!=nil{return nil,err};raw=append([]byte(nil),raw...)
  if len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=plan.Ref.SHA256{return nil,v2input.ErrInvalid}
  consumed+=len(raw);if consumed>128<<20{return nil,v2input.ErrInvalid}
  if err:=v2draft.ValidateCorrectionPlan(raw,plan.Binding);err!=nil{return nil,err}
  var doc struct{Items []struct{Binding v2input.SessionVersionBinding `json:"binding"`} `json:"items"`}
  if err:=json.Unmarshal(raw,&doc);err!=nil{return nil,err}
  for _,row:=range doc.Items{
   old:=row.Binding
   if existing,ok:=byVersion[old.AssetVersionID];ok{if existing.Old!=old{return nil,v2input.ErrInvalid};continue}
   source,ok:=q.archive[old.Source.ArtifactID]
   if !ok||source.contract!="WorkingImage"||source.sha!=old.Source.SHA256{return nil,v2input.ErrInvalid}
   sourceRaw,err:=q.reader.ReadImportArchiveDocument(ctx,source.staged,1<<20);if err!=nil{return nil,err}
   if len(sourceRaw)<2||len(sourceRaw)>1<<20||rawSHA(sourceRaw)!=old.Source.SHA256{return nil,v2input.ErrInvalid}
   image,err:=review.ParseWorkingImage(sourceRaw);if err!=nil{return nil,err}
   index,err:=uniqueImportCorrectionSource(parsed,sources,image.Canvas.SourceSHA256);if err!=nil{return nil,err}
   current:=sources.document.Entries[index]
   if _,exists:=byEntry[current.ID];exists{return nil,v2input.ErrInvalid}
   master,err:=q.masters.ReadImportMaster(ctx,*current.Master,1<<20);if err!=nil{return nil,err}
   if len(master)<2||len(master)>1<<20||rawSHA(master)!=current.Master.SHA256{return nil,v2input.ErrInvalid}
   canonicalImage,err:=review.ParseWorkingImage(master);if err!=nil{return nil,err}
   canvas,err:=canonical.Hash(canonicalImage.Canvas);if err!=nil{return nil,err}
   target:=v2input.SessionVersionBinding{AssetVersionID:*current.Version,Source:*current.Master,CanvasHash:canvas}
   if err:=q.QualifyImportRecipeSource(ctx,current.ID,old,target);err!=nil{return nil,err}
   item:=ImportCorrectionBinding{Entry:current.ID,Old:old,New:target}
   if len(result)>=5000{return nil,v2input.ErrInvalid}
   byVersion[old.AssetVersionID]=item;byEntry[current.ID]=item;result=append(result,item)
  }
  current,err:=q.reader.ReadImportArchiveDocument(ctx,archived.staged,8<<20);if err!=nil{return nil,err}
  if !bytes.Equal(raw,current){return nil,v2input.ErrInvalid}
  pins=append(pins,planPin{ref:archived.staged,size:len(raw)})
 }
 // Later sibling/plan IO must not leave an earlier plan witness stale.
 for _,pin:=range pins{
  if err:=ctx.Err();err!=nil{return nil,err}
  current,err:=q.reader.ReadImportArchiveDocument(ctx,pin.ref,8<<20);if err!=nil{return nil,err}
  if len(current)!=pin.size||rawSHA(current)!=pin.ref.SHA256{return nil,v2input.ErrInvalid}
 }
 return result,ctx.Err()
}

// An image hash is enough only when it selects one adopted manifest entry.
// Duplicate originals need an explicit portable entry mapping; never guess.
func uniqueImportCorrectionSource(parsed importParsedDocument,sources ImportSourceInventory,sha string)(int,error){
 if !validImportRefForSHA(sha)||len(parsed.Manifest.Entries)!=len(sources.document.Entries){return 0,v2input.ErrInvalid}
 selected:=-1
 for i,entry:=range parsed.Manifest.Entries{
  source:=sources.document.Entries[i]
  if source.ID!=entry.ID{return 0,v2input.ErrInvalid}
  if entry.ImageSHA!=sha||source.State!="adopted"{continue}
  if source.Master==nil||source.Version==nil||*source.Version==uuid.Nil||!validImportRef(*source.Master){return 0,v2input.ErrInvalid}
  if selected>=0{return 0,&ImportRecipeUnsupported{Reason:"correction_source_entry_ambiguous"}}
  selected=i
 }
 if selected<0{return 0,&ImportRecipeUnsupported{Reason:"correction_source_not_imported"}}
 return selected,nil
}
