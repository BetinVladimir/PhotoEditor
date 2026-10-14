package interchange
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2portrait"
)
// Project is replaced from the actual workflow before SQL installation.
type ImportLocalPortraitInstallation struct { Project uuid.UUID; RuntimeSHA string; Capacity v2portrait.LocalResourceEstimate }
func(i ImportLocalPortraitInstallation)Build(ctx context.Context,base *ImportOperationRouter,masters ImportMasterReader,documents ImportPreparedDocumentReader,masks ImportMaskQualifier,runtime ImportLocalRuntimeQualification)(*ImportOperationRouter,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if !validImportRefForSHA(i.RuntimeSHA)||masters==nil||documents==nil||masks==nil||runtime==nil{return nil,v2input.ErrInvalid}
 if i.Capacity.PixelCount<1||i.Capacity.RAMBytes<1||i.Capacity.ScratchBytes<1||i.Capacity.OutputBytes<1||i.Capacity.VRAMBytes<0{return nil,v2input.ErrInvalid}
 operations:=map[ImportOperationVersion]ImportLayerOperationQualifier{}
 if base!=nil{for key,validator:=range base.operations{operations[key]=validator}}
 qualifier:=ImportLocalPortraitOperationQualifier{Masters:masters,Documents:documents,Masks:masks,Runtime:runtime,RuntimeSHA:i.RuntimeSHA,Capacity:i.Capacity}
 for _,operation:=range []string{"portrait.hair","portrait.clothing"}{
  key:=ImportOperationVersion{operation,"1"}
  if _,exists:=operations[key];exists{return nil,v2input.ErrInvalid}
  operations[key]=qualifier
 }
 return NewImportOperationRouter(ctx,operations)
}
