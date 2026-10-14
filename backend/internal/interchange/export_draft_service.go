package interchange

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
)

// PrepareExportDraft pins the recovery evidence and creates draft containers in
// one caller-owned transaction. Existing ledger evidence wins on replay; callers
// must use the returned documents for subsequent object publication.
func PrepareExportDraft(ctx context.Context,tx PendingTransaction,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,proposed FrozenExportDocuments)(FrozenExportDocuments,CompiledExportPlan,error){
 frozen,plan,err:=PinExportPending(ctx,tx,who,tid,request,key,proposed)
 if err!=nil{return FrozenExportDocuments{},CompiledExportPlan{},err}
 if err:=frozen.BeginExportRun(ctx,tx,who,tid,request,key);err!=nil{return FrozenExportDocuments{},CompiledExportPlan{},err}
 return frozen,plan,nil
}

// No object IO occurs while holding the database transaction. A successful draft
// is private and is neither a creation ticket nor a sealed/runnable workflow.
func(s Service)PrepareExportDraft(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,proposed FrozenExportDocuments)(FrozenExportDocuments,CompiledExportPlan,error){
 if s.DB==nil{return FrozenExportDocuments{},CompiledExportPlan{},domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
 var frozen FrozenExportDocuments;var plan CompiledExportPlan
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var err error;frozen,plan,err=PrepareExportDraft(ctx,tx,who,tid,request,key,proposed);return err
 })
 // Do not expose a draft if the transaction failed during commit.
 if err!=nil{return FrozenExportDocuments{},CompiledExportPlan{},err}
 return frozen,plan,nil
}
