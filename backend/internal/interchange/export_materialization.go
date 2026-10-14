package interchange

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type SQLExportTaskMaterializer struct{
 DB *pgxpool.Pool
 Tenant uuid.UUID
 Who auth.Identity
 Request CreateRequest
 Key string
 Inventory ExportInputInventory
 Resources ExportTaskResources
}

func(s SQLExportTaskMaterializer)MaterializeExport(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,task v2input.DraftTask)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil{return domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
 expectedIntent,err:=FreezeCreateIntent(s.Tenant,s.Who,s.Request,s.Key);if err!=nil{return err}
 if intent.Identity!=expectedIntent.Identity||intent.RequestSHA!=expectedIntent.RequestSHA||!bytes.Equal(intent.RequestJCS,expectedIntent.RequestJCS){return v2input.ErrInvalid}
 expected,err:=CompileExportTask(ctx,expectedIntent,frozen,s.Inventory,s.Resources);if err!=nil{return err}
 raw,err:=canonical.Marshal(task);if err!=nil{return err}
 expectedRaw,err:=canonical.Marshal(expected);if err!=nil||!bytes.Equal(raw,expectedRaw){return v2input.ErrInvalid}
 _,compiled,err:=DecodeFrozenExportDocuments(ctx,expectedIntent,frozen.Body());if err!=nil{return err}
 return tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var accepted bool
  if err:=tx.QueryRow(ctx,`SELECT photo.materialize_v30_interchange_export($1,$2,$3,$4,$5,$6,$7::jsonb)`,s.Tenant,s.Who.UserID,s.Who.SessionID,s.Key,expectedIntent.RequestSHA,string(compiled.paramsBody),string(raw)).Scan(&accepted);err!=nil{return err}
  if !accepted{return domain.NewError("INTERCHANGE_CREATE_STALE",409)}
  return nil
 })
}
