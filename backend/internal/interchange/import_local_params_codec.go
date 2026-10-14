package interchange

import (
 "bytes"
 "context"
 "encoding/json"

 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2portrait"
)

// Local Params preserve authored controls; source and masks get new identities.
// The operation qualifier must still prove installed runtime and physical masks.
type ImportLocalPortraitParamsCodec struct {
 Qualifier ImportLayerOperationQualifier
 Fallback ImportDependencyCodec
}

func localImportParams(raw []byte)(string,v2input.SessionVersionBinding,v2input.Ref,v2input.Ref,error){
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return "",v2input.SessionVersionBinding{},v2input.Ref{},v2input.Ref{},v2input.ErrInvalid}
 _,hair:=fields["detail"];_,clothing:=fields["wrinkle_strength"]
 if hair==clothing{return "",v2input.SessionVersionBinding{},v2input.Ref{},v2input.Ref{},&ImportRecipeUnsupported{Reason:"params_contract_not_installed"}}
 var compiled []byte;var err error
 var base v2input.SessionVersionBinding;var mask,guard v2input.Ref;var operation string
 if hair{var p v2portrait.HairParams;if err=json.Unmarshal(raw,&p);err!=nil{return "",base,mask,guard,err};compiled,_,err=p.Compile();base,mask,guard=p.Base,p.Mask,p.Protected;operation="portrait.hair"}else{var p v2portrait.ClothingParams;if err=json.Unmarshal(raw,&p);err!=nil{return "",base,mask,guard,err};compiled,_,err=p.Compile();base,mask,guard=p.Base,p.Mask,p.Protected;operation="portrait.clothing"}
 if err!=nil{return "",base,mask,guard,err};if !bytes.Equal(compiled,raw){return "",base,mask,guard,v2input.ErrInvalid}
 return operation,base,mask,guard,nil
}

func(c ImportLocalPortraitParamsCodec)RemapImportDocument(ctx context.Context,kind string,raw []byte,old,target v2input.SessionVersionBinding,resolve func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if kind!="Params"{if c.Fallback!=nil{return c.Fallback.RemapImportDocument(ctx,kind,raw,old,target,resolve)};return nil,&ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 // Own authored bytes before source/mask callbacks can mutate caller storage.
 raw=append([]byte(nil),raw...)
 operation,base,mask,guard,err:=localImportParams(raw)
 if unsupported,ok:=err.(*ImportRecipeUnsupported);ok&&c.Fallback!=nil&&unsupported!=nil{return c.Fallback.RemapImportDocument(ctx,kind,raw,old,target,resolve)}
 if err!=nil{return nil,err}
 if c.Qualifier==nil||resolve==nil||base!=old{return nil,v2input.ErrInvalid}
 if old.CanvasHash!=target.CanvasHash{return nil,&ImportRecipeUnsupported{Reason:"params_canvas_remap_required"}}
 mappedMask,err:=resolve(mask,"MaskRef");if err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err}
 mappedGuard,err:=resolve(guard,"MaskRef");if err!=nil{return nil,err}
 if !validImportRef(mappedMask)||!validImportRef(mappedGuard)||mappedMask.ArtifactID==mask.ArtifactID||mappedGuard.ArtifactID==guard.ArtifactID{return nil,v2input.ErrInvalid}
 var prepared []byte
 if operation=="portrait.hair"{var p v2portrait.HairParams;if err=json.Unmarshal(raw,&p);err!=nil{return nil,err};p.Base,p.Mask,p.Protected=target,mappedMask,mappedGuard;prepared,_,err=p.Compile()}else{var p v2portrait.ClothingParams;if err=json.Unmarshal(raw,&p);err!=nil{return nil,err};p.Base,p.Mask,p.Protected=target,mappedMask,mappedGuard;prepared,_,err=p.Compile()}
 if err!=nil{return nil,err}
 if err=c.QualifyRemappedImportDocument(ctx,kind,prepared,target);err!=nil{return nil,err};return prepared,ctx.Err()
}

func(c ImportLocalPortraitParamsCodec)QualifyRemappedImportDocument(ctx context.Context,kind string,raw []byte,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if kind!="Params"{if c.Fallback!=nil{return c.Fallback.QualifyRemappedImportDocument(ctx,kind,raw,target)};return &ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 operation,base,_,_,err:=localImportParams(raw)
 if _,ok:=err.(*ImportRecipeUnsupported);ok&&c.Fallback!=nil{return c.Fallback.QualifyRemappedImportDocument(ctx,kind,raw,target)}
 if err!=nil{return err};if c.Qualifier==nil||base!=target{return v2input.ErrInvalid}
 if err:=c.Qualifier.QualifyImportedLayerOperation(ctx,operation,"1",append([]byte(nil),raw...),target);err!=nil{return err};return ctx.Err()
}

func(c ImportLocalPortraitParamsCodec)BindImportDocumentSource(source v2input.Ref)ImportDependencyCodec{
 if binder,ok:=c.Fallback.(ImportDocumentSourceBinder);ok{c.Fallback=binder.BindImportDocumentSource(source)}
 return c
}
