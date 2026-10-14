package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)
type ImportUpscaleQualifications map[int]ImportUpscaleQualificationInstallation
type importUpscaleRuntimeRouter struct{scales map[int]ImportLayerOperationQualifier}
func(i ImportUpscaleQualifications)owned()(ImportUpscaleQualifications,error){
 if len(i)<1||len(i)>2{return nil,v2input.ErrInvalid}
 result:=ImportUpscaleQualifications{}
 for scale,config:=range i{if scale!=2&&scale!=4{return nil,v2input.ErrInvalid};result[scale]=config.owned()}
 return result,nil
}
func(i ImportUpscaleQualifications)Build(ctx context.Context,store SQLPreparedImportArtifactStore,objects ImportDocumentObjects)(ImportLayerOperationQualifier,map[int]string,error){
 if err:=ctx.Err();err!=nil{return nil,nil,err}
 owned,err:=i.owned();if err!=nil{return nil,nil,err}
 router:=importUpscaleRuntimeRouter{scales:map[int]ImportLayerOperationQualifier{}};locks:=map[int]string{}
 // Stable installation order is independent of Go map iteration.
 for _,scale:=range []int{2,4}{config,ok:=owned[scale];if !ok{continue};policy,lock,err:=config.Build(ctx,store,objects);if err!=nil{return nil,nil,err};router.scales[scale]=policy;locks[scale]=lock}
 return router,locks,ctx.Err()
}
func(q importUpscaleRuntimeRouter)QualifyImportedLayerOperation(ctx context.Context,operation,version string,raw []byte,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if operation!="image.upscale"||version!="1"{return v2input.ErrInvalid}
 params,err:=decodeImportUpscaleParams(raw);if err!=nil{return err}
 policy:=q.scales[params.Scale];if policy==nil{return &ImportRecipeUnsupported{Reason:"upscale_runtime_not_installed"}}
 if err:=policy.QualifyImportedLayerOperation(ctx,operation,version,append([]byte(nil),raw...),binding);err!=nil{return err};return ctx.Err()
}
