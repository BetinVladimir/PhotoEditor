package interchange

import (
 "bytes"
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type ImportBinaryRemapper interface{RemapImportBinary(context.Context,uuid.UUID,v2input.Ref,v2input.Ref,string,v2input.SessionVersionBinding,v2input.SessionVersionBinding,int64)(v2input.Ref,int64,error)}
// Publisher copies the exact immutable version with expected bytes/SHA and
// preserves those bytes; no normalization, quantization or mask resampling.
type ImportBinaryPublisher interface{PublishImportBinary(context.Context,uuid.UUID,string,v2input.Ref,objectstore.Object)(v2input.Ref,error)}
type ImportMaskBinaryRemapper struct{Workflow uuid.UUID;Catalog ImportDocumentCatalog;Objects ImportDocumentObjects;Masters ImportMasterReader;Publisher ImportBinaryPublisher}
func(r ImportMaskBinaryRemapper)RemapImportBinary(ctx context.Context,entry uuid.UUID,archived,staged v2input.Ref,kind string,old,target v2input.SessionVersionBinding,maxBytes int64)(v2input.Ref,int64,error){
 invalid:=func()(v2input.Ref,int64,error){return v2input.Ref{},0,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return v2input.Ref{},0,err}
 if kind!="MaskPixels"{return v2input.Ref{},0,&ImportRecipeUnsupported{Reason:"binary_dependency_codec_not_installed"}}
 if r.Workflow==uuid.Nil||entry==uuid.Nil||r.Catalog==nil||r.Objects==nil||r.Masters==nil||r.Publisher==nil||!validImportRef(archived)||!validImportRef(staged)||archived.SHA256!=staged.SHA256||maxBytes<1||maxBytes>128<<30||old.CanvasHash!=target.CanvasHash{return invalid()}
 master,err:=r.Masters.ReadImportMaster(ctx,target.Source,1<<20);if err!=nil{return v2input.Ref{},0,err};master=append([]byte(nil),master...)
 if len(master)<2||len(master)>1<<20||rawSHA(master)!=target.Source.SHA256{return invalid()}
 var typed review.WorkingImage;if err:=decodeImportDocument(master,&typed);err!=nil{return v2input.Ref{},0,err}
 image,err:=review.ParseWorkingImage(master);if err!=nil{return v2input.Ref{},0,err}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return v2input.Ref{},0,err};if canvas!=target.CanvasHash{return invalid()}
 node:=v2input.Entry{Ref:staged,Contract:"Artifact"}
 object,err:=r.Catalog.LookupImportDocument(ctx,node);if err!=nil{return v2input.Ref{},0,err}
 if object.Key==""||object.VersionID==""||object.VersionID=="null"||object.SHA256!=staged.SHA256||object.Bytes<1||object.Bytes>maxBytes||object.ContentType!="application/octet-stream"{return invalid()}
 reader,err:=r.Objects.Open(ctx,object);if err!=nil{return v2input.Ref{},0,err};if reader==nil{return invalid()}
 verifyErr:=masknpy.VerifyFloat32MaskNPY(ctx,reader,object.Bytes,object.SHA256,int64(image.Canvas.Width),int64(image.Canvas.Height));closeErr:=reader.Close()
 if verifyErr!=nil{return v2input.Ref{},0,verifyErr};if closeErr!=nil{return v2input.Ref{},0,closeErr}
 if err:=ctx.Err();err!=nil{return v2input.Ref{},0,err}
 current,err:=r.Catalog.LookupImportDocument(ctx,node);if err!=nil{return v2input.Ref{},0,err};if current!=object{return invalid()}
 mapped:=v2input.Ref{ArtifactID:uuid.NewSHA1(r.Workflow,[]byte("interchange:dependency:"+entry.String()+":"+kind+":"+archived.ArtifactID.String())),SHA256:archived.SHA256}
 if mapped.ArtifactID==archived.ArtifactID{return invalid()}
 receipt,err:=r.Publisher.PublishImportBinary(ctx,entry,kind,mapped,object);if err!=nil{return v2input.Ref{},0,err};if receipt!=mapped{return invalid()}
 if err:=ctx.Err();err!=nil{return v2input.Ref{},0,err}
 current,err=r.Catalog.LookupImportDocument(ctx,node);if err!=nil{return v2input.Ref{},0,err};if current!=object{return invalid()}
 currentMaster,err:=r.Masters.ReadImportMaster(ctx,target.Source,1<<20);if err!=nil{return v2input.Ref{},0,err};if !bytes.Equal(currentMaster,master){return invalid()}
 return mapped,object.Bytes,ctx.Err()
}
