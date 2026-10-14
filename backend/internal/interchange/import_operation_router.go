package interchange

import (
 "context"
 "bytes"
 "github.com/google/uuid"
 "strings"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type ImportOperationVersion struct { Operation, Version string }
// Registration is operator configuration, never imported document content.
// Validators must still prove full Params, dependencies and current admission.
type ImportOperationRouter struct { operations map[ImportOperationVersion]ImportLayerOperationQualifier }
func NewImportOperationRouter(ctx context.Context,installed map[ImportOperationVersion]ImportLayerOperationQualifier)(*ImportOperationRouter,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if len(installed)==0||len(installed)>1024{return nil,v2input.ErrInvalid}
 router:=&ImportOperationRouter{operations:make(map[ImportOperationVersion]ImportLayerOperationQualifier,len(installed))}
 for key,validator:=range installed{
  if strings.TrimSpace(key.Operation)!=key.Operation||key.Operation==""||len(key.Operation)>128||strings.TrimSpace(key.Version)!=key.Version||key.Version==""||len(key.Version)>32||validator==nil{return nil,v2input.ErrInvalid}
  router.operations[key]=validator
 }
 return router,ctx.Err()
}
func(r *ImportOperationRouter)QualifyImportedLayerOperation(ctx context.Context,operation,version string,raw []byte,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if r==nil{return v2input.ErrInvalid}
 validator:=r.operations[ImportOperationVersion{operation,version}]
 if validator==nil{return &ImportRecipeUnsupported{Reason:"layer_operation_not_installed"}}
 if len(raw)<2||len(raw)>8<<20||binding.AssetVersionID==uuid.Nil||!validImportRef(binding.Source)||!validImportRefForSHA(binding.CanvasHash){return v2input.ErrInvalid}
 // Freeze before calling external code. Params semantic validation is delegated
 // only to the exact registered implementation; normalization is not approval.
 owned,err:=canonical.Bytes(append([]byte(nil),raw...));if err!=nil{return err};if !bytes.Equal(owned,raw){return v2input.ErrInvalid}
 if err:=validator.QualifyImportedLayerOperation(ctx,operation,version,owned,binding);err!=nil{return err}
 return ctx.Err()
}
var _ ImportLayerOperationQualifier=(*ImportOperationRouter)(nil)
