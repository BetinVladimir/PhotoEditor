package interchange

import (
 "context"
 "io"
 "strings"
 "unicode/utf8"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func validateImportBackgroundOccurrence(scope ImportPreparedScope,ref v2input.Ref)error{
 if scope.Background==nil{if scope.Contract=="StyleBackgroundAsset"{return v2input.ErrInvalid};return nil}
 source:=*scope.Background
 if scope.Contract!="StyleBackgroundAsset"||scope.Workflow==uuid.Nil||scope.Entry==uuid.Nil||scope.Binding.AssetVersionID==uuid.Nil||!validImportRef(scope.Binding.Source)||!validImportRef(v2input.Ref{ArtifactID:scope.Entry,SHA256:scope.Binding.CanvasHash})||!validImportRef(ref)||scope.Warp!=nil||!validImportRef(source.Style)||!validImportRef(source.Original)||!validImportRef(source.Staged)||source.Original.SHA256!=source.Staged.SHA256||ref.SHA256!=source.Original.SHA256||source.Portable==""||len(source.Portable)>120||!utf8.ValidString(source.Portable)||strings.TrimSpace(source.Portable)!=source.Portable||ref.ArtifactID!=importBackgroundID(scope.Workflow,source,scope.Binding){return v2input.ErrInvalid}
 return nil
}
// Uses existing immutable streaming publication, quota reservation, fencing,
// conflict recovery and SQL complete. The certificate retains the local ID.
type PreparedImportBackgroundCommitter struct{
 Publisher PreparedImportArtifactPublisher
 Catalog ImportDocumentCatalog
 Authority ImportBackgroundTargetAuthority
}
func(c PreparedImportBackgroundCommitter)CommitImportedBackground(ctx context.Context,source ImportBackgroundSource,object objectstore.Object,target v2input.SessionVersionBinding,id,media string)(string,error){
 if err:=ctx.Err();err!=nil{return "",err}
 if c.Catalog==nil||c.Authority==nil||c.Publisher.SourceObjects==nil||target!=c.Publisher.Scope.Binding{return "",v2input.ErrInvalid}
 scope,err:=c.Publisher.scope(c.Publisher.Scope.Entry,"StyleBackgroundAsset");if err!=nil{return "",err}
 scope.Background=&source
 ref:=v2input.Ref{ArtifactID:importBackgroundID(scope.Workflow,source,target),SHA256:source.Original.SHA256}
 if id!=ref.ArtifactID.String(){return "",v2input.ErrInvalid}
 if err:=validateImportBackgroundOccurrence(scope,ref);err!=nil{return "",err}
 if object.SHA256!=ref.SHA256||object.ContentType!="application/octet-stream"||object.Bytes<8||object.Bytes>536870912{return "",v2input.ErrInvalid}
 switch media{case "image/jpeg","image/png","image/tiff":default:return "",v2input.ErrInvalid}
 if err:=c.Authority.QualifyImportBackgroundTarget(ctx,target);err!=nil{return "",err}
 current,err:=c.Catalog.LookupImportDocument(ctx,v2input.Entry{Ref:source.Staged,Contract:"Artifact"});if err!=nil{return "",err};if current!=object{return "",v2input.ErrInvalid}
 _,err=c.Publisher.publish(ctx,scope,ref,object.Bytes,media,func()(io.ReadCloser,error){return c.Publisher.SourceObjects.Open(ctx,object)})
 if err!=nil{return "",err}
 return id,ctx.Err()
}
