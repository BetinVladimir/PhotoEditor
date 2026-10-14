package interchange
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// Validate every mapping before IO, then independently qualify each archived /
// current pair. This catalogue includes correction siblings, not only the root.
func qualifyImportEntrySources(ctx context.Context,scope ImportPreparedScope,document importParsedDocument,bindings []ImportCorrectionBinding,sources ImportRecipeSourceQualifier)(map[uuid.UUID]importWorkingEvidence,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if sources==nil||len(bindings)<1||len(bindings)>5000{return nil,v2input.ErrInvalid}
 owned:=append([]ImportCorrectionBinding(nil),bindings...)
 originals:=map[uuid.UUID]string{}
 for _,entry:=range document.Manifest.Entries{
  if entry.ID==uuid.Nil||!validImportRefForSHA(entry.ImageSHA){return nil,v2input.ErrInvalid}
  if _,ok:=originals[entry.ID];ok{return nil,v2input.ErrInvalid};originals[entry.ID]=entry.ImageSHA
 }
 result:=map[uuid.UUID]importWorkingEvidence{}
 entries:=map[uuid.UUID]bool{};versions:=map[uuid.UUID]bool{};oldVersions:=map[uuid.UUID]bool{}
 root:=false
 valid:=func(b v2input.SessionVersionBinding)bool{return b.AssetVersionID!=uuid.Nil&&validImportRef(b.Source)&&validImportRefForSHA(b.CanvasHash)}
 for _,item:=range owned{
  original,ok:=originals[item.Entry]
  if !ok||entries[item.Entry]||!valid(item.Old)||!valid(item.New)||versions[item.New.AssetVersionID]||oldVersions[item.Old.AssetVersionID]||item.Old.AssetVersionID==item.New.AssetVersionID||item.Old.Source.ArtifactID==item.New.Source.ArtifactID{return nil,v2input.ErrInvalid}
  if _,ok:=result[item.New.Source.ArtifactID];ok{return nil,v2input.ErrInvalid}
  entries[item.Entry]=true;versions[item.New.AssetVersionID]=true;oldVersions[item.Old.AssetVersionID]=true
  result[item.New.Source.ArtifactID]=importWorkingEvidence{ref:item.New.Source,canvas:item.New.CanvasHash,original:original}
  if item.Entry==scope.Entry{if item.New!=scope.Binding{return nil,v2input.ErrInvalid};root=true}
 }
 if !root{return nil,v2input.ErrInvalid}
 for _,item:=range owned{
  if err:=ctx.Err();err!=nil{return nil,err}
  if err:=sources.QualifyImportRecipeSource(ctx,item.Entry,item.Old,item.New);err!=nil{return nil,err}
 }
 return result,ctx.Err()
}
