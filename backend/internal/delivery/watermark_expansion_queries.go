package delivery
import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
const watermarkExpansionPlanQuery=`SELECT photo.v30_delivery_expansion_plan($1,$2)`
// Closed, lease-bound adapter. Every query commits before returning its value;
// no source/physical object read can retain these authorization row locks.
type watermarkExpansionQueries struct{db MetadataTransactionBeginner;lease ExpansionLease}
type watermarkExpansionRow struct{owner watermarkExpansionQueries;ctx context.Context;query string;args []any}
func(q watermarkExpansionQueries)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{return watermarkExpansionRow{q,ctx,query,append([]any(nil),args...)}}
func(r watermarkExpansionRow)Scan(dest ...any)error{
 if err:=r.ctx.Err();err!=nil{return err};l:=r.owner.lease
 if r.owner.db==nil||!l.valid(){return v2input.ErrInvalid}
 expected:=[]uuid.UUID{}
 switch r.query{
 case watermarkExpansionScopeQuery:expected=[]uuid.UUID{l.Tenant,l.Run,l.Expansion,l.Token,l.ParentTask};if len(r.args)!=5{return v2input.ErrInvalid}
 case watermarkExpansionPlanQuery:expected=[]uuid.UUID{l.Expansion,l.Token};if len(r.args)!=2{return v2input.ErrInvalid}
 case watermarkPinReadQuery:expected=[]uuid.UUID{l.Tenant,l.Run,l.Expansion,l.Token};if len(r.args)!=6{return v2input.ErrInvalid};preset,ok:=r.args[4].(uuid.UUID);sha,valid:=r.args[5].(string);if !ok||preset==uuid.Nil||!valid||!shaPattern.MatchString(sha){return v2input.ErrInvalid}
 default:return v2input.ErrInvalid
 }
 for n,wanted:=range expected{got,ok:=r.args[n].(uuid.UUID);if !ok||got!=wanted{return v2input.ErrInvalid}}
 ctx,cancel:=context.WithTimeout(r.ctx,30*time.Second);defer cancel()
 tx,err:=r.owner.db.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err}
 defer func(){cleanup,stop:=context.WithTimeout(context.WithoutCancel(r.ctx),5*time.Second);defer stop();_=tx.Rollback(cleanup)}()
 if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,l.Tenant.String());err!=nil{return err}
 if err=tx.QueryRow(ctx,r.query,r.args...).Scan(dest...);err!=nil{return err}
 if err=ctx.Err();err!=nil{return err};if err=tx.Commit(ctx);err!=nil{return err};return r.ctx.Err()
}
// LoadPersistedWatermarkPlan derives identity from current SQL scope and repeats
// it after plan/pin restoration. Final child writes must reauthorize in their TX.
func LoadPersistedWatermarkPlan(ctx context.Context,db MetadataTransactionBeginner,lease ExpansionLease)(PreparePlan,WatermarkPinScope,error){
 var zero PreparePlan;var empty WatermarkPinScope
 if err:=ctx.Err();err!=nil{return zero,empty,err};if db==nil||!lease.valid(){return zero,empty,v2input.ErrInvalid}
 queries:=watermarkExpansionQueries{db:db,lease:lease};store:=ExpansionLeaseStore{DB:queries}
 scope,err:=store.ReadWatermarkScope(ctx,lease);if err!=nil{return zero,empty,err}
 plan,err:=store.LoadWatermarkPlan(ctx,lease,scope,WatermarkPinStore{DB:queries});if err!=nil{return zero,empty,err}
 fresh,err:=store.ReadWatermarkScope(ctx,lease);if err!=nil{return zero,empty,err};if fresh!=scope{return zero,empty,v2input.ErrInvalid}
 return plan,scope,ctx.Err()
}
