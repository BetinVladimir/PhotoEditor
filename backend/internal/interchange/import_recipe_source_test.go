package interchange

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestImportRecipeSourceResolvesArchivedUUIDThroughInventoryAndCurrentMaster(t *testing.T){
 for _,change:=range []string{"none","unknown_entry","unknown_archive","archive_hash","old_canvas","new_canvas","new_hash","same_version","same_master","archive_changed","master_changed","cancelled"}{t.Run(change,func(t *testing.T){
  _,master,masterRaw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(masterRaw);if err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  parsed:=importParsedValidatorFixture();parsed.Manifest.Entries[0].ImageSHA=image.Canvas.SourceSHA256;parsed.Manifest.Inventory[0].SHA=image.Canvas.SourceSHA256;parsed.Files[0].Content.SHA256=image.Canvas.SourceSHA256
  archivedID,stagedID:=uuid.New(),uuid.New();path:="documents/source.json"
  parsed.Manifest.Inventory=append(parsed.Manifest.Inventory,importBundleInventory{ID:archivedID,Path:path,SHA:master.Ref.SHA256,Contract:"WorkingImage"})
  parsed.Files=append(parsed.Files,importParsedFile{Path:path,Content:v2input.Ref{ArtifactID:stagedID,SHA256:master.Ref.SHA256},Contract:"WorkingImage"})
  raw,err:=canonical.Marshal(parsed);if err!=nil{t.Fatal(err)}
  old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:archivedID,SHA256:master.Ref.SHA256},CanvasHash:canvas}
  target:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:master.Ref,CanvasHash:canvas}
  archive:=&sourceArchiveReader{expected:v2input.Ref{ArtifactID:stagedID,SHA256:master.Ref.SHA256},body:masterRaw}
  current:=&recipeSourceMasterReader{body:masterRaw}
  qualifier,err:=NewParsedImportRecipeSourceQualifier(raw,archive,current);if err!=nil{t.Fatal(err)}
  entry:=parsed.Manifest.Entries[0].ID
  switch change{
  case "unknown_entry":entry=uuid.New()
  case "unknown_archive":old.Source.ArtifactID=uuid.New()
  case "archive_hash":archive.body=[]byte(`{}`)
  case "old_canvas":old.CanvasHash=rawSHA([]byte("foreign canvas"))
  case "new_canvas":target.CanvasHash=rawSHA([]byte("foreign canvas"))
  case "new_hash":current.body=[]byte(`{}`)
  case "same_version":target.AssetVersionID=old.AssetVersionID
  case "same_master":target.Source=old.Source
  case "archive_changed":archive.second=[]byte(`{}`)
  case "master_changed":current.second=[]byte(`{}`)
  }
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if change=="cancelled"{cancel()}
  err=qualifier.QualifyImportRecipeSource(ctx,entry,old,target)
  if change=="none"{if err!=nil||archive.calls!=2||current.calls!=2{t.Fatal("source qualification",err)};return}
  if err==nil{t.Fatal("foreign source accepted")}
  if change=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
 })}
}
type sourceArchiveReader struct{expected v2input.Ref;body []byte;calls int;second []byte}
func(s *sourceArchiveReader)ReadImportArchiveDocument(_ context.Context,ref v2input.Ref,limit int64)([]byte,error){s.calls++;if ref!=s.expected||limit!=1<<20{return nil,v2input.ErrInvalid};if s.calls>1&&s.second!=nil{return s.second,nil};return s.body,nil}


type recipeSourceMasterReader struct{body,second []byte;calls int}
func(s *recipeSourceMasterReader)ReadImportMaster(_ context.Context,_ v2input.Ref,_ int64)([]byte,error){
 s.calls++;if s.calls>1&&s.second!=nil{return s.second,nil};return s.body,nil
}
