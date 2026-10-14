package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// Implementations must physically qualify the exact staged image, enforce
// current tenant/project authority and quota, publish it idempotently, and
// register the requested portable ID before returning it. An archive mapping
// alone never authorizes publication.
type ImportBackgroundPublisher interface {
 PublishImportedBackground(context.Context,ImportBackgroundSource,v2input.SessionVersionBinding,string)(string,error)
}

type IndexedImportBackgroundRemapper struct {
 Workflow uuid.UUID
 Index *ImportBackgroundIndex
 Publisher ImportBackgroundPublisher
}

func(r IndexedImportBackgroundRemapper)RemapImportedStyleBackground(ctx context.Context,style v2input.Ref,portable string,target v2input.SessionVersionBinding)(string,error){
 if err:=ctx.Err();err!=nil{return "",err}
 if r.Workflow==uuid.Nil||r.Index==nil||r.Publisher==nil||target.AssetVersionID==uuid.Nil||!validImportRef(target.Source)||!validImportRef(v2input.Ref{ArtifactID:target.AssetVersionID,SHA256:target.CanvasHash}){return "",v2input.ErrInvalid}
 source,err:=r.Index.ResolveImportBackground(ctx,style,portable);if err!=nil{return "",err}
 source.Style=style;source.Portable=portable
 if !validImportRef(source.Original)||!validImportRef(source.Staged)||source.Original.SHA256!=source.Staged.SHA256{return "",v2input.ErrInvalid}
 // Namespace by workflow and exact target, style and original asset pins.
 // Staging UUID is deliberately excluded so a resumed staging pass is stable.
 id:=importBackgroundID(r.Workflow,source,target).String()
 if id==portable{return "",v2input.ErrInvalid}
 receipt,err:=r.Publisher.PublishImportedBackground(ctx,source,target,id);if err!=nil{return "",err}
 if err:=ctx.Err();err!=nil{return "",err}
 if receipt!=id{return "",v2input.ErrInvalid}
 return id,nil
}

func importBackgroundID(workflow uuid.UUID,source ImportBackgroundSource,target v2input.SessionVersionBinding)uuid.UUID{
 return uuid.NewSHA1(workflow,[]byte("interchange:background:"+target.AssetVersionID.String()+":"+target.Source.ArtifactID.String()+":"+target.Source.SHA256+":"+target.CanvasHash+":"+source.Style.ArtifactID.String()+":"+source.Style.SHA256+":"+source.Portable+":"+source.Original.ArtifactID.String()+":"+source.Original.SHA256))
}
