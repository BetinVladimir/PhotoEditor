package interchange

import (
 "bytes"
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2portrait"
)

type ImportLocalRuntimeQualification interface{QualifyImportLocalRuntime(context.Context,string,string)error}
// Reuses the current V28 qualification report/policy authority. The short
// transaction is an observation; graph admission must lock qualification again.
type SQLImportLocalRuntimeQualification struct{DB *pgxpool.Pool;Tenant,Project uuid.UUID}
func(s SQLImportLocalRuntimeQualification)QualifyImportLocalRuntime(ctx context.Context,operation,runtimeSHA string)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||s.Tenant==uuid.Nil||s.Project==uuid.Nil{return v2input.ErrInvalid}
 return database.TenantTransaction(ctx,s.DB,s.Tenant,func(tx pgx.Tx)error{return v2portrait.LockLocalRuntimeQualification(ctx,tx,s.Tenant,s.Project,operation,runtimeSHA)})
}

type ImportLocalPortraitOperationQualifier struct{
 Masters ImportMasterReader
 Documents ImportPreparedDocumentReader
 Masks ImportMaskQualifier
 Runtime ImportLocalRuntimeQualification
 RuntimeSHA string
 Capacity v2portrait.LocalResourceEstimate
 Fallback ImportLayerOperationQualifier
}
func(q ImportLocalPortraitOperationQualifier)QualifyImportedLayerOperation(ctx context.Context,operation,version string,raw []byte,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if operation!="portrait.hair"&&operation!="portrait.clothing"{if q.Fallback!=nil{return q.Fallback.QualifyImportedLayerOperation(ctx,operation,version,raw,binding)};return &ImportRecipeUnsupported{Reason:"layer_operation_not_installed"}}
 if version!="1"||q.Masters==nil||q.Documents==nil||q.Masks==nil||q.Runtime==nil{return v2input.ErrInvalid}
 actual,base,mask,guard,err:=localImportParams(append([]byte(nil),raw...));if err!=nil{return err}
 if actual!=operation||base!=binding{return v2input.ErrInvalid}
 if err:=q.Runtime.QualifyImportLocalRuntime(ctx,operation,q.RuntimeSHA);err!=nil{return err}
 master,err:=q.Masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err};master=append([]byte(nil),master...)
 if rawSHA(master)!=binding.Source.SHA256{return v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(master);if err!=nil{return err}
 hash,err:=canonical.Hash(image.Canvas);if err!=nil||hash!=binding.CanvasHash||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"{return v2input.ErrInvalid}
 if _,err:=v2portrait.CompileLocalExecutionProfile(binding,master,q.RuntimeSHA,q.Capacity);err!=nil{return err}
 refs:=[]v2input.Ref{mask,guard};bodies:=make([][]byte,0,2)
 for _,ref:=range refs{
  body,err:=q.Documents.ReadPreparedImportDocument(ctx,v2input.Entry{Ref:ref,Contract:"MaskRef"},1<<20);if err!=nil{return err}
  if rawSHA(body)!=ref.SHA256{return v2input.ErrInvalid};body=append([]byte(nil),body...)
  typed,err:=review.ParseMaskRef(body);if err!=nil{return err};if typed.Canvas!=image.Canvas{return v2input.ErrInvalid}
  if err:=q.Masks.QualifyImportedMask(ctx,typed,binding);err!=nil{return err};bodies=append(bodies,body)
 }
 for i,ref:=range refs{body,err:=q.Documents.ReadPreparedImportDocument(ctx,v2input.Entry{Ref:ref,Contract:"MaskRef"},1<<20);if err!=nil{return err};if !bytes.Equal(body,bodies[i]){return v2input.ErrInvalid}}
 current,err:=q.Masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err};if !bytes.Equal(current,master){return v2input.ErrInvalid}
 if err:=q.Runtime.QualifyImportLocalRuntime(ctx,operation,q.RuntimeSHA);err!=nil{return err};return ctx.Err()
}
