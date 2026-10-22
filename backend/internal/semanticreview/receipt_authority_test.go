package semanticreview

import (
 "context"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

type semanticAuthorityRow struct { identity ReviewIdentity; err error }
func(r semanticAuthorityRow)Scan(dst ...any)error{
 if r.err!=nil{return r.err}
 *(dst[0].(*uuid.UUID))=r.identity.RunID
 *(dst[1].(*uuid.UUID))=r.identity.WorkflowID
 *(dst[2].(*uuid.UUID))=r.identity.ProjectID
 return nil
}
type semanticAuthoritySQL struct { pgx.Tx; current ReviewIdentity; err error; reads int }
func(tx *semanticAuthoritySQL)QueryRow(context.Context,string,...any)pgx.Row{tx.reads++;return semanticAuthorityRow{identity:tx.current,err:tx.err}}
func TestSemanticReceiptWaitRequiresCurrentIdentity(t *testing.T){
 identity:=ReviewIdentity{RunID:uuid.New(),WorkflowID:uuid.New(),ProjectID:uuid.New()}
 for _,mode:=range []string{"same","project_changed","revoked","canceled"}{
  t.Run(mode,func(t *testing.T){
   sql:=&semanticAuthoritySQL{current:identity}
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   switch mode{case "project_changed":sql.current.ProjectID=uuid.New();case "revoked":sql.err=pgx.ErrNoRows;case "canceled":cancel()}
   guarded:=guardReceiptAuthority(sql,uuid.New(),uuid.New(),uuid.New(),uuid.New(),identity).(receiptAuthorityTx)
   err:=guarded.ValidateReceiptAuthority(ctx)
   if (err==nil)!=(mode=="same"){t.Fatalf("mode=%s err=%v",mode,err)}
   if mode=="canceled" && sql.reads!=0{t.Fatal("canceled guard touched database")}
  })
 }
}
