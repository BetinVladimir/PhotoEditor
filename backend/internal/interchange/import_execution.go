package interchange

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/v2input"
)

type ImportInputRegistrar interface {
 RegisterImportInputs(context.Context, ImportTransport, []byte) error
}

type ImportExecutionPreparation struct {
 Documents ImportDocumentPublisher
 Resolver ImportClosureNodeResolver
 Materializer ImportTaskMaterializer
 Transport v2input.Publisher
 Registrar ImportInputRegistrar
}

// Prepare publishes immutable roots, resolves their complete physical closure,
// persists a blocked shell, then publishes and registers transport documents.
// No database transaction spans object IO. A failed stage leaves durable,
// replayable receipts; it never activates the task or returns an accepted job.
func (p ImportExecutionPreparation) Prepare(ctx context.Context, tenant uuid.UUID, documents ImportPreparedDocuments, task v2input.DraftTask, parsed []byte) error {
 if err:=ctx.Err();err!=nil{return err}
 if tenant==uuid.Nil||documents.evidence==nil||p.Documents==nil||p.Resolver==nil||p.Materializer==nil||p.Transport==nil||p.Registrar==nil{return v2input.ErrInvalid}
 if _,err:=importParamsRoots(documents);err!=nil{return err}
 task.ExecutionProfile=append(json.RawMessage(nil),task.ExecutionProfile...)
 parsed=append([]byte(nil),parsed...)
 if err:=validateImportTask(documents,task);err!=nil{return err}
 if len(parsed)<2||len(parsed)>8<<20{return v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(parsed);if err!=nil||!bytes.Equal(normalized,parsed){return v2input.ErrInvalid}
 var params struct{Parsed v2input.Ref `json:"parsed"`}
 if err:=json.Unmarshal(documents.params.Body,&params);err!=nil||params.Parsed.SHA256!=rawSHA(parsed){return v2input.ErrInvalid}
 if _,err:=documents.Publish(ctx,p.Documents);err!=nil{return err}
 inventory,err:=ResolveImportInputInventory(ctx,tenant,documents,p.Resolver);if err!=nil{return err}
 if inventory.InputDigest()!=task.InputDigest{return v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return err}
 if err:=p.Materializer.MaterializeImport(ctx,documents,task);err!=nil{return err}
 transport,err:=inventory.PublishInputs(ctx,p.Transport);if err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 return p.Registrar.RegisterImportInputs(ctx,transport,parsed)
}

// SQLImportInputRegistrar commits the entire batch-and-seal sequence together.
// Every refusal rolls back previously registered batches in this transaction.
type SQLImportInputRegistrar struct{DB *pgxpool.Pool;Tenant,Run uuid.UUID}
func(s SQLImportInputRegistrar)RegisterImportInputs(ctx context.Context,transport ImportTransport,parsed []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||s.Tenant==uuid.Nil||s.Run==uuid.Nil||transport.inventory.tenant!=s.Tenant||transport.inventory.documents.identity.Run!=s.Run{return v2input.ErrInvalid}
 parsed=append([]byte(nil),parsed...)
 return database.TenantTransaction(ctx,s.DB,s.Tenant,func(tx pgx.Tx)error{
  return transport.RegisterInputs(ctx,tx,s.Tenant,s.Run,parsed)
 })
}
