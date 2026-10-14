package interchange

import (
 "context"
 "strings"
 "unicode/utf8"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// Authority rechecks the live import workflow, selected entry and target
// binding under current tenant/project ACL. It must not trust archive claims.
type ImportBackgroundTargetAuthority interface {
 QualifyImportBackgroundTarget(context.Context,v2input.SessionVersionBinding)error
}
// Commit copies exact qualified bytes under a fenced quota reservation and
// atomically registers the portable ID with the import's prepared style scope.
// Implementations must recheck the source object and target authority at commit.
type ImportBackgroundCommitter interface {
 CommitImportedBackground(context.Context,ImportBackgroundSource,objectstore.Object,v2input.SessionVersionBinding,string,string)(string,error)
}

type QualifiedImportBackgroundPublisher struct {
 Catalog ImportDocumentCatalog
 Native ImportBackgroundRasterQualifier
 Authority ImportBackgroundTargetAuthority
 Committer ImportBackgroundCommitter
}

func(p QualifiedImportBackgroundPublisher)PublishImportedBackground(ctx context.Context,source ImportBackgroundSource,target v2input.SessionVersionBinding,id string)(string,error){
 if err:=ctx.Err();err!=nil{return "",err}
 if p.Catalog==nil||p.Native==nil||p.Authority==nil||p.Committer==nil||!validImportRef(source.Original)||!validImportRef(source.Staged)||source.Original.SHA256!=source.Staged.SHA256||id==""||len(id)>120||!utf8.ValidString(id)||strings.TrimSpace(id)!=id{return "",v2input.ErrInvalid}
 if err:=p.Authority.QualifyImportBackgroundTarget(ctx,target);err!=nil{return "",err}
 // Staged import documents are admitted by the scoped catalogue as Artifact.
 node:=v2input.Entry{Ref:source.Staged,Contract:"Artifact"}
 object,err:=p.Catalog.LookupImportDocument(ctx,node);if err!=nil{return "",err}
 if object.Key==""||object.VersionID==""||object.VersionID=="null"||object.SHA256!=source.Staged.SHA256||object.Bytes<1||object.Bytes>536870912{return "",v2input.ErrInvalid}
 if object.ContentType!="application/octet-stream"{return "",v2input.ErrInvalid}
 // Reuse the installed native raster decoder and resource limits. It preserves
 // encoded bytes and does not normalize or alter processing parameters.
 media,err:=p.Native.QualifyImportBackgroundRaster(ctx,source.Staged,object);if err!=nil{return "",err}
 switch media{case "image/jpeg","image/png","image/tiff":default:return "",v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return "",err}
 current,err:=p.Catalog.LookupImportDocument(ctx,node);if err!=nil{return "",err}
 if current!=object{return "",v2input.ErrInvalid}
 if err:=p.Authority.QualifyImportBackgroundTarget(ctx,target);err!=nil{return "",err}
 receipt,err:=p.Committer.CommitImportedBackground(ctx,source,object,target,id,media);if err!=nil{return "",err}
 if err:=ctx.Err();err!=nil{return "",err}
 if receipt!=id{return "",v2input.ErrInvalid}
 return receipt,nil
}
