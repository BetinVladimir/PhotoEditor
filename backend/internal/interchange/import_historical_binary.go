package interchange
import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// Copies independently authenticated historical arrays without resampling,
// normalization or replacing them with the current master.
type ImportHistoricalBinaryRemapper struct{
 Authority ImportHistoricalNativeAuthority
 Catalog ImportDocumentCatalog
 Objects ImportDocumentObjects
 Publisher ImportBinaryPublisher
}
func(r ImportHistoricalBinaryRemapper)RemapImportBinary(ctx context.Context,entry uuid.UUID,archived,staged v2input.Ref,kind string,old,target v2input.SessionVersionBinding,maxBytes int64)(v2input.Ref,int64,error){
 if err:=ctx.Err();err!=nil{return v2input.Ref{},0,err}
 q:=r.Authority
 if r.Catalog==nil||r.Objects==nil||r.Publisher==nil||q.Catalog.Scope.Entry!=entry||q.Old!=old||q.Target!=target||!validImportRef(archived)||!validImportRef(staged)||staged.SHA256!=archived.SHA256||maxBytes<1{return v2input.Ref{},0,v2input.ErrInvalid}
 if kind!="WorkingPixels"&&kind!="WorkingAlpha"&&kind!="DenseWarpField"{return v2input.Ref{},0,v2input.ErrInvalid}
 var parsed importParsedDocument;if decodeImportDocument(q.Catalog.Store.Parsed,&parsed)!=nil{return v2input.Ref{},0,v2input.ErrInvalid}
 width,height:=int64(0),int64(0);found:=false
 for _,item:=range parsed.Manifest.Inventory{
  if item.Contract!="WorkingImage"{continue};candidate:=q;candidate.source=v2input.Ref{ArtifactID:item.ID,SHA256:item.SHA}
  _,raw,err:=candidate.original(ctx,"WorkingImage");if err!=nil{return v2input.Ref{},0,err}
  image,err:=review.ParseWorkingImage(raw);if err!=nil{return v2input.Ref{},0,err}
  matches:=kind=="WorkingPixels"&&image.Pixels.ID==archived.ArtifactID&&image.Pixels.SHA256==archived.SHA256
  if kind=="WorkingAlpha"{matches=image.Alpha!=nil&&image.Alpha.ID==archived.ArtifactID&&image.Alpha.SHA256==archived.SHA256}
  w,h:=int64(image.Canvas.Width),int64(image.Canvas.Height)
  if kind=="DenseWarpField"{var chain struct{Steps []struct{Kind string `json:"kind"`;Field v2input.Ref `json:"inverse_field"`;Canvas struct{Width,Height int64} `json:"to_canvas"`} `json:"steps"`};if json.Unmarshal(image.TransformChain,&chain)!=nil{return v2input.Ref{},0,v2input.ErrInvalid};for _,step:=range chain.Steps{if step.Kind=="dense_warp"&&step.Field==archived{matches=true;w,h=step.Canvas.Width,step.Canvas.Height}}}
  if !matches{continue};if err=candidate.QualifyArchivedHistoricalDocument(ctx,"WorkingImage",raw,old,target);err!=nil{return v2input.Ref{},0,err}
  if found&&(width!=w||height!=h){return v2input.Ref{},0,v2input.ErrInvalid};found=true;width,height=w,h
 }
 if !found||width<1||height<1||width>200000000/height{return v2input.Ref{},0,v2input.ErrInvalid}
 node:=v2input.Entry{Ref:staged,Contract:"Artifact"};object,err:=r.Catalog.LookupImportDocument(ctx,node);if err!=nil{return v2input.Ref{},0,err}
 if object.SHA256!=archived.SHA256||object.ContentType!="application/octet-stream"||object.Bytes<1||object.Bytes>maxBytes||object.VersionID==""||object.VersionID=="null"{return v2input.Ref{},0,v2input.ErrInvalid}
 stream,err:=r.Objects.Open(ctx,object);if err!=nil{return v2input.Ref{},0,err};if stream==nil{return v2input.Ref{},0,v2input.ErrInvalid}
 if kind=="WorkingAlpha"{err=masknpy.VerifyAlphaNPY(ctx,stream,object.Bytes,object.SHA256,width,height)}else{channels:=3;if kind=="DenseWarpField"{channels=2};err=masknpy.VerifyFloat32ArrayNPY(ctx,stream,object.Bytes,object.SHA256,width,height,channels,false)}
 closeErr:=stream.Close();if err!=nil{return v2input.Ref{},0,err};if closeErr!=nil{return v2input.Ref{},0,closeErr}
 current,err:=r.Catalog.LookupImportDocument(ctx,node);if err!=nil{return v2input.Ref{},0,err};if current!=object{return v2input.Ref{},0,v2input.ErrInvalid}
 mapped:=v2input.Ref{ArtifactID:uuid.NewSHA1(q.Catalog.Scope.Workflow,[]byte("interchange:dependency:"+entry.String()+":"+kind+":"+archived.ArtifactID.String())),SHA256:archived.SHA256}
 receipt,err:=r.Publisher.PublishImportBinary(ctx,entry,kind,mapped,object);if err!=nil{return v2input.Ref{},0,err};if receipt!=mapped{return v2input.Ref{},0,v2input.ErrInvalid}
 current,err=r.Catalog.LookupImportDocument(ctx,node);if err!=nil{return v2input.Ref{},0,err};if current!=object{return v2input.Ref{},0,v2input.ErrInvalid};return mapped,object.Bytes,ctx.Err()
}
