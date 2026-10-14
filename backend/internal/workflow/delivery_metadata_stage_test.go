package workflow

import (
 "context"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

type metadataStageObjects struct{artifactjson.Objects} // Any IO during replay is a test failure.
type metadataStagePool struct{calls,commits int}
type metadataStageTx struct{pgx.Tx;pool *metadataStagePool}
type metadataStageRow struct{}
func(p *metadataStagePool)BeginTx(context.Context,pgx.TxOptions)(pgx.Tx,error){p.calls++;return &metadataStageTx{pool:p},nil}
func(t *metadataStageTx)Exec(context.Context,string,...any)(pgconn.CommandTag,error){return pgconn.NewCommandTag("SELECT 1"),nil}
func(t *metadataStageTx)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if q!=`SELECT photo.v30_metadata_task_activated($1,$2,$3,$4)`{panic("replay performed materialization")}
 return metadataStageRow{}
}
func(metadataStageRow)Scan(dest ...any)error{*dest[0].(*bool)=true;return nil}
func(t *metadataStageTx)Commit(context.Context)error{t.pool.commits++;return nil}
func(t *metadataStageTx)Rollback(context.Context)error{return nil}
func assertMetadataStageActivatedReplay(t *testing.T,spec DeliveryMetadataSpec,registry Registry,limits DeliveryDerivativeLimits){
 t.Helper();pool:=&metadataStagePool{}
 workflow,_:=v2input.WorkflowID(spec.ChildRun);parent,_:=v2input.TaskID(workflow,"export.encode",0)
 lease:=delivery.MetadataBarrierLease{Tenant:uuid.New(),ChildRun:spec.ChildRun,Expansion:uuid.New(),Token:uuid.New(),ParentTask:parent,Until:time.Now().Add(time.Minute)}
 service:=DeliveryMetadataStage{DB:pool,Objects:metadataStageObjects{},Bucket:"photos",Registry:registry,Limits:limits}
 if err:=service.Stage(context.Background(),lease,spec,4096);err!=nil||pool.calls!=1||pool.commits!=1{t.Fatal("activated stage replay",err,pool.calls)}
 wrong:=lease;wrong.ParentTask=uuid.New()
 if err:=service.Stage(context.Background(),wrong,spec,4096);err==nil||pool.calls!=1{t.Fatal("foreign producer reached DB")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=service.Stage(ctx,lease,spec,4096);err!=context.Canceled||pool.calls!=1{t.Fatal("cancelled stage reached DB",err)}
}
