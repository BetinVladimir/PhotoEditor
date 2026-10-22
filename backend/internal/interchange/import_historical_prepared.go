package interchange
import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type historicalArrayBinding struct{Parent,Original v2input.Ref;Width,Height int64}
type ImportPreparedHistoricalArrayQualifier struct{
 Authority ImportHistoricalNativeAuthority
 Objects ImportDocumentObjects
 Documents ImportClosureNodeResolver
 Bindings map[v2input.Entry][]historicalArrayBinding
}
func(q ImportPreparedHistoricalArrayQualifier)QualifyExportBinary(ctx context.Context,entry v2input.Entry,object objectstore.Object)error{
 if err:=ctx.Err();err!=nil{return err};bindings:=q.Bindings[entry]
 if len(bindings)==0||q.Objects==nil||q.Documents==nil||object.SHA256!=entry.Ref.SHA256||object.ContentType!="application/octet-stream"{return v2input.ErrInvalid}
 for _,binding:=range bindings{
  authority:=q.Authority;authority.source=binding.Parent
  _,original,err:=authority.original(ctx,"WorkingImage");if err!=nil{return err}
  if err=authority.QualifyArchivedHistoricalDocument(ctx,"WorkingImage",original,authority.Old,authority.Target);err!=nil{return err}
  current,err:=authority.preparedRef(ctx,binding.Original,entry.Contract);if err!=nil{return err};if current!=entry.Ref{return v2input.ErrInvalid}
  parent,err:=authority.preparedRef(ctx,binding.Parent,"WorkingImage");if err!=nil{return err}
  node,err:=q.Documents.ResolveImportNode(ctx,v2input.Entry{Ref:parent,Contract:"WorkingImage"});if err!=nil{return err}
  if node.Entry.Ref!=parent||rawSHA(node.Body)!=parent.SHA256||!preparedPNGChild(node.Children,entry){return v2input.ErrInvalid}
  stream,err:=q.Objects.Open(ctx,object);if err!=nil{return err};if stream==nil{return v2input.ErrInvalid}
  if entry.Contract=="WorkingAlpha"{err=masknpy.VerifyAlphaNPY(ctx,stream,object.Bytes,object.SHA256,binding.Width,binding.Height)}else{channels:=3;if entry.Contract=="DenseWarpField"{channels=2};err=masknpy.VerifyFloat32ArrayNPY(ctx,stream,object.Bytes,object.SHA256,binding.Width,binding.Height,channels,false)}
  closeErr:=stream.Close();if err!=nil{return err};if closeErr!=nil{return closeErr}
 }
 return ctx.Err()
}
func historicalPreparedArrayBindings(ctx context.Context,q ImportHistoricalNativeAuthority)(map[v2input.Entry][]historicalArrayBinding,error){
 var parsed importParsedDocument;if decodeImportDocument(q.Catalog.Store.Parsed,&parsed)!=nil{return nil,v2input.ErrInvalid}
 result:=map[v2input.Entry][]historicalArrayBinding{};var total int64
 for _,item:=range parsed.Manifest.Inventory{
  if item.Contract!="WorkingImage"{continue};authority:=q;authority.source=v2input.Ref{ArtifactID:item.ID,SHA256:item.SHA}
  parent,raw,err:=authority.original(ctx,"WorkingImage");if err!=nil{return nil,err};total+=int64(len(raw));if total>128<<20{return nil,v2input.ErrInvalid}
  if parent==q.Old.Source{continue}
  image,err:=review.ParseWorkingImage(raw);if err!=nil{return nil,err}
  remember:=func(ref v2input.Ref,kind string,w,h int64)error{
   if !validImportRef(ref)||w<1||h<1||w>200000000/h{return v2input.ErrInvalid}
   mapped:=v2input.Ref{ArtifactID:uuid.NewSHA1(q.Catalog.Scope.Workflow,[]byte("interchange:dependency:"+q.Catalog.Scope.Entry.String()+":"+kind+":"+ref.ArtifactID.String())),SHA256:ref.SHA256}
   entry:=v2input.Entry{Ref:mapped,Contract:kind};result[entry]=append(result[entry],historicalArrayBinding{parent,ref,w,h});return nil
  }
  if err=remember(v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},"WorkingPixels",int64(image.Canvas.Width),int64(image.Canvas.Height));err!=nil{return nil,err}
  if image.Alpha!=nil{if err=remember(v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256},"WorkingAlpha",int64(image.Canvas.Width),int64(image.Canvas.Height));err!=nil{return nil,err}}
  var chain struct{Steps []struct{Kind string `json:"kind"`;Field v2input.Ref `json:"inverse_field"`;Canvas struct{Width,Height int64} `json:"to_canvas"`} `json:"steps"`};if json.Unmarshal(image.TransformChain,&chain)!=nil{return nil,v2input.ErrInvalid}
  for _,step:=range chain.Steps{if step.Kind=="dense_warp"{if err=remember(step.Field,"DenseWarpField",step.Canvas.Width,step.Canvas.Height);err!=nil{return nil,err}}}
 }
 return result,ctx.Err()
}
type importEntryHistoricalBinaryRouter struct{entries map[uuid.UUID]ImportHistoricalBinaryRemapper}
func(r importEntryHistoricalBinaryRouter)RemapImportBinary(ctx context.Context,entry uuid.UUID,archived,staged v2input.Ref,kind string,old,target v2input.SessionVersionBinding,maxBytes int64)(v2input.Ref,int64,error){selected,ok:=r.entries[entry];if !ok{return v2input.Ref{},0,v2input.ErrInvalid};return selected.RemapImportBinary(ctx,entry,archived,staged,kind,old,target,maxBytes)}
