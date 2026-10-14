package interchange
import (
 "bytes"
 "context"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type ExportBackgroundQualifier struct{
 Reader ExportClosureNodeResolver
 Parents []v2input.Entry
 Authority ExportStyleBackgroundAuthority
 Images ExportBinaryQualifier
}
func(q ExportBackgroundQualifier)QualifyExportBinary(ctx context.Context,entry v2input.Entry,object objectstore.Object)error{
 if err:=ctx.Err();err!=nil{return err}
 if entry.Contract!="StyleBackgroundAsset"||!validImportRef(entry.Ref)||q.Reader==nil||q.Authority==nil||q.Images==nil||len(q.Parents)<1||len(q.Parents)>v2input.MaxEntries{return v2input.ErrInvalid}
 matched:=false
 for _,parent:=range q.Parents{
  if parent.Contract!="FrozenRenderRecipe"||!validImportRef(parent.Ref){return v2input.ErrInvalid}
  node,err:=q.Reader.ResolveExportNode(ctx,parent);if err!=nil{return err}
  body:=append([]byte(nil),node.Body...)
  if node.Entry!=parent||rawSHA(body)!=parent.Ref.SHA256{return v2input.ErrInvalid}
  recipe,err:=v2draft.DecodeFrozenRenderRecipe(body);if err!=nil{return err}
  pin:=recipe.BackgroundAsset;if pin==nil||pin.Source!=entry.Ref{continue}
  if pin.ExpectedBytes!=object.Bytes||pin.MediaType!=object.ContentType||object.SHA256!=entry.Ref.SHA256{return v2input.ErrInvalid}
  found:=false;for _,child:=range node.Children{if child.Ref.ArtifactID==entry.Ref.ArtifactID{if child!=entry{return v2input.ErrInvalid};found=true}}
  if !found{return v2input.ErrInvalid}
  if err:=q.Authority.QualifyExportStyleBackground(ctx,*pin);err!=nil{return err}
  if err:=q.Images.QualifyExportBinary(ctx,v2input.Entry{Ref:entry.Ref,Contract:"EncodedImage"},object);err!=nil{return err}
  current,err:=q.Reader.ResolveExportNode(ctx,parent);if err!=nil{return err}
  if current.Entry!=parent||!bytes.Equal(current.Body,body){return v2input.ErrInvalid}
  if err:=q.Authority.QualifyExportStyleBackground(ctx,*pin);err!=nil{return err};matched=true
 }
 if !matched{return v2input.ErrInvalid};return ctx.Err()
}
