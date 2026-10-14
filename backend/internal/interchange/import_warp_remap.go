package interchange

import (
 "bytes"
 "context"
 "regexp"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type ImportDenseWarpRemapper interface{RemapImportedDenseWarp(context.Context,v2input.Ref,review.Canvas,review.Canvas,v2input.SessionVersionBinding)(v2input.Ref,error)}
type ImportOccurrenceWarpPublisher interface{PublishImportWarp(context.Context,uuid.UUID,v2input.Ref,objectstore.Object,v2input.Ref,review.Canvas,review.Canvas)(v2input.Ref,error)}
type ImportedDenseWarpRemapper struct{
 workflow,entry uuid.UUID
 original string
 files map[uuid.UUID]importArchivedSource
 catalog ImportDocumentCatalog
 objects ImportDocumentObjects
 masters ImportMasterReader
 publisher ImportBinaryPublisher
 maxBytes int64
}
func NewImportedDenseWarpRemapper(workflow,entry uuid.UUID,parsedRaw []byte,catalog ImportDocumentCatalog,objects ImportDocumentObjects,masters ImportMasterReader,publisher ImportBinaryPublisher,maxBytes int64)(*ImportedDenseWarpRemapper,error){
 if workflow==uuid.Nil||entry==uuid.Nil||catalog==nil||objects==nil||masters==nil||publisher==nil||maxBytes<1||maxBytes>128<<30{return nil,v2input.ErrInvalid}
 if _,err:=validateParsedImportDocument(context.Background(),parsedRaw);err!=nil{return nil,err}
 var parsed importParsedDocument;if err:=decodeImportDocument(parsedRaw,&parsed);err!=nil{return nil,err}
 original:="";for _,item:=range parsed.Manifest.Entries{if item.ID==entry{original=item.ImageSHA}}
 if original==""{return nil,v2input.ErrInvalid}
 staged:=map[string]v2input.Ref{};for _,file:=range parsed.Files{staged[file.Path]=file.Content}
 files:=map[uuid.UUID]importArchivedSource{}
 for _,item:=range parsed.Manifest.Inventory{if _,ok:=files[item.ID];ok{return nil,v2input.ErrInvalid};files[item.ID]=importArchivedSource{staged:staged[item.Path],sha:item.SHA,contract:item.Contract}}
 return &ImportedDenseWarpRemapper{workflow:workflow,entry:entry,original:original,files:files,catalog:catalog,objects:objects,masters:masters,publisher:publisher,maxBytes:maxBytes},nil
}
var importWarpSpace=regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$`)
func(r *ImportedDenseWarpRemapper)RemapImportedDenseWarp(ctx context.Context,ref v2input.Ref,from,to review.Canvas,target v2input.SessionVersionBinding)(v2input.Ref,error){
 if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
 if r==nil||r.catalog==nil||r.objects==nil||r.masters==nil||r.publisher==nil||target.AssetVersionID==uuid.Nil||!validImportRef(ref)||!validImportRef(target.Source){return v2input.Ref{},v2input.ErrInvalid}
 valid:=func(c review.Canvas)bool{return c.Width>=1&&c.Height>=1&&c.Width<=200000000&&c.Height<=200000000/c.Width&&c.Orientation==1&&importWarpSpace.MatchString(c.SpaceID)&&c.SourceSHA256==r.original}
 if !valid(from)||!valid(to){return v2input.Ref{},v2input.ErrInvalid}
 file,ok:=r.files[ref.ArtifactID];if !ok||file.sha!=ref.SHA256||file.contract!="DenseWarpField"{return v2input.Ref{},v2input.ErrInvalid}
 master,err:=r.masters.ReadImportMaster(ctx,target.Source,1<<20);if err!=nil{return v2input.Ref{},err};master=append([]byte(nil),master...)
 if len(master)<2||len(master)>1<<20||rawSHA(master)!=target.Source.SHA256{return v2input.Ref{},v2input.ErrInvalid}
 var typed review.WorkingImage;if err:=decodeImportDocument(master,&typed);err!=nil{return v2input.Ref{},err}
 image,err:=review.ParseWorkingImage(master);if err!=nil{return v2input.Ref{},err};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return v2input.Ref{},err}
 if canvas!=target.CanvasHash||image.Canvas.SourceSHA256!=r.original||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"{return v2input.Ref{},v2input.ErrInvalid}
 node:=v2input.Entry{Ref:file.staged,Contract:"Artifact"}
 object,err:=r.catalog.LookupImportDocument(ctx,node);if err!=nil{return v2input.Ref{},err}
 if object.Key==""||object.VersionID==""||object.VersionID=="null"||object.ContentType!="application/octet-stream"||object.SHA256!=ref.SHA256||object.Bytes<1||object.Bytes>r.maxBytes{return v2input.Ref{},v2input.ErrInvalid}
 reader,err:=r.objects.Open(ctx,object);if err!=nil{if reader!=nil{_ = reader.Close()};return v2input.Ref{},err};if reader==nil{return v2input.Ref{},v2input.ErrInvalid}
 verifyErr:=masknpy.VerifyFloat32ArrayNPY(ctx,reader,object.Bytes,ref.SHA256,to.Width,to.Height,2,false);closeErr:=reader.Close()
 if verifyErr!=nil{return v2input.Ref{},verifyErr};if closeErr!=nil{return v2input.Ref{},closeErr}
 current,err:=r.catalog.LookupImportDocument(ctx,node);if err!=nil{return v2input.Ref{},err};if current!=object{return v2input.Ref{},v2input.ErrInvalid}
 occurrence,err:=canonical.Hash(struct{From,To review.Canvas;Target v2input.SessionVersionBinding}{from,to,target});if err!=nil{return v2input.Ref{},err}
 mapped:=v2input.Ref{ArtifactID:uuid.NewSHA1(r.workflow,[]byte("interchange:warp:"+r.entry.String()+":"+ref.ArtifactID.String()+":"+occurrence)),SHA256:ref.SHA256}
 if mapped.ArtifactID==ref.ArtifactID{return v2input.Ref{},v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
 var receipt v2input.Ref
 if publisher,ok:=r.publisher.(ImportOccurrenceWarpPublisher);ok{receipt,err=publisher.PublishImportWarp(ctx,r.entry,mapped,object,ref,from,to)}else{receipt,err=r.publisher.PublishImportBinary(ctx,r.entry,"DenseWarpField",mapped,object)}
 if err!=nil{return v2input.Ref{},err};if receipt!=mapped{return v2input.Ref{},v2input.ErrInvalid}
 current,err=r.catalog.LookupImportDocument(ctx,node);if err!=nil{return v2input.Ref{},err};if current!=object{return v2input.Ref{},v2input.ErrInvalid}
 currentMaster,err:=r.masters.ReadImportMaster(ctx,target.Source,1<<20);if err!=nil{return v2input.Ref{},err};if !bytes.Equal(master,currentMaster){return v2input.Ref{},v2input.ErrInvalid}
 return mapped,ctx.Err()
}
