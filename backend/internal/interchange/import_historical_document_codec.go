package interchange

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type ImportHistoricalGeometryResolver interface{ResolveHistoricalGeometryField(context.Context,v2input.Ref)(v2input.Ref,error)}

type ImportHistoricalDocumentAuthority interface {
 QualifyArchivedHistoricalDocument(context.Context,string,[]byte,v2input.SessionVersionBinding,v2input.SessionVersionBinding)error
 QualifyPreparedHistoricalDocument(context.Context,string,[]byte,v2input.SessionVersionBinding)error
}
// Historical masters retain historical pixels. Only immutable transport refs
// and object identities are remapped; current master pixels never substitute.
type ImportHistoricalDocumentCodec struct{Authority ImportHistoricalDocumentAuthority}
func(c ImportHistoricalDocumentCodec)RemapImportDocument(ctx context.Context,kind string,raw []byte,old,target v2input.SessionVersionBinding,resolve func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if c.Authority==nil||resolve==nil{return nil,&ImportRecipeUnsupported{Reason:"historical_producer_authority_not_installed"}}
 if kind!="WorkingImage"&&kind!="FaceLandmarks"&&kind!="FaceBoxes"{return nil,v2input.ErrInvalid}
 if err:=c.Authority.QualifyArchivedHistoricalDocument(ctx,kind,append([]byte(nil),raw...),old,target);err!=nil{return nil,err}
 var fields map[string]json.RawMessage;if decodeImportDocument(raw,&fields)!=nil{return nil,v2input.ErrInvalid}
 remap:=func(values map[string]json.RawMessage,key,role string)error{
  var original v2input.Ref;if json.Unmarshal(values[key],&original)!=nil||!validImportRef(original){return v2input.ErrInvalid}
  var mapped v2input.Ref;var err error
  if geometry,ok:=c.Authority.(ImportHistoricalGeometryResolver);ok&&role=="DenseWarpField"{mapped,err=geometry.ResolveHistoricalGeometryField(ctx,original)}else{mapped,err=resolve(original,role)};if err!=nil{return err};values[key],err=canonical.Marshal(mapped);return err
 }
 remapChain:=func(values map[string]json.RawMessage)error{
  var chain map[string]json.RawMessage;if json.Unmarshal(values["transform_chain"],&chain)!=nil{return v2input.ErrInvalid}
  var steps []map[string]json.RawMessage;if json.Unmarshal(chain["steps"],&steps)!=nil{return v2input.ErrInvalid}
  for _,step:=range steps{var kind string;_ = json.Unmarshal(step["kind"],&kind);if kind=="dense_warp"{if err:=remap(step,"inverse_field","DenseWarpField");err!=nil{return err}}}
  chain["steps"],_=canonical.Marshal(steps);values["transform_chain"],_=canonical.Marshal(chain);return nil
 }
 if kind=="WorkingImage"{
  if _,err:=review.ParseWorkingImage(raw);err!=nil{return nil,err}
  if err:=remap(fields,"pixels","WorkingPixels");err!=nil{return nil,err}
  if string(fields["alpha"])!="null"{if err:=remap(fields,"alpha","WorkingAlpha");err!=nil{return nil,err}}
  if err:=remapChain(fields);err!=nil{return nil,err}
 }else{
  if err:=remap(fields,"image","WorkingImage");err!=nil{return nil,err}
  list:="faces";if kind=="FaceLandmarks"{list="landmarks";if err:=remap(fields,"faces","FaceBoxes");err!=nil{return nil,err}}
  var items []map[string]json.RawMessage;if json.Unmarshal(fields[list],&items)!=nil{return nil,v2input.ErrInvalid}
  for _,item:=range items{if kind=="FaceLandmarks"{if err:=remapChain(item);err!=nil{return nil,err}};var id uuid.UUID;if json.Unmarshal(item["object_id"],&id)!=nil||id==uuid.Nil{return nil,v2input.ErrInvalid};item["object_id"],_=canonical.Marshal(uuid.NewSHA1(target.AssetVersionID,[]byte("interchange:mask-object:"+id.String())))}
  fields[list],_=canonical.Marshal(items)
 }
 prepared,err:=canonical.Marshal(fields);if err!=nil{return nil,err}
 if err:=c.Authority.QualifyPreparedHistoricalDocument(ctx,kind,append([]byte(nil),prepared...),target);err!=nil{return nil,err}
 return prepared,ctx.Err()
}
func(c ImportHistoricalDocumentCodec)QualifyRemappedImportDocument(ctx context.Context,kind string,raw []byte,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err};if c.Authority==nil{return &ImportRecipeUnsupported{Reason:"historical_producer_authority_not_installed"}}
 if kind!="WorkingImage"&&kind!="FaceLandmarks"&&kind!="FaceBoxes"{return v2input.ErrInvalid}
 return c.Authority.QualifyPreparedHistoricalDocument(ctx,kind,append([]byte(nil),raw...),target)
}

type ImportHistoricalSourceBinder interface{BindHistoricalDocumentSource(v2input.Ref)ImportHistoricalDocumentAuthority}
func(c ImportHistoricalDocumentCodec)BindImportDocumentSource(source v2input.Ref)ImportDependencyCodec{
 binder,ok:=c.Authority.(ImportHistoricalSourceBinder);if !ok||!validImportRef(source){return nil}
 authority:=binder.BindHistoricalDocumentSource(source);if authority==nil{return nil};return ImportHistoricalDocumentCodec{Authority:authority}
}
