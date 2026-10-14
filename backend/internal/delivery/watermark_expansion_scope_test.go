package delivery
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
type watermarkScopeDB struct{project,actor uuid.UUID;calls int;args []any;missing bool}
func(d *watermarkScopeDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if q!=watermarkExpansionScopeQuery{panic("scope query")};d.calls++;d.args=append([]any(nil),args...)
 return recoveryRow{scan:func(dest ...any)error{if d.missing{return pgx.ErrNoRows};*dest[0].(*uuid.UUID)=d.project;*dest[1].(*uuid.UUID)=d.actor;return nil}}
}
func TestWatermarkExpansionScopeABIAndRefusals(t *testing.T){
 lease:=validExpansionLease();db:=&watermarkScopeDB{project:uuid.New(),actor:uuid.New()};store:=ExpansionLeaseStore{DB:db}
 scope,err:=store.ReadWatermarkScope(context.Background(),lease);if err!=nil||scope.Tenant!=lease.Tenant||scope.Parent!=lease.Run||scope.Project!=db.project||scope.Actor!=db.actor||len(db.args)!=5||db.args[4]!=lease.ParentTask{t.Fatal("scope",err)}
 db.missing=true;if _,err:=store.ReadWatermarkScope(context.Background(),lease);err==nil{t.Fatal("stale scope")}
 before:=db.calls;foreign:=lease;foreign.Token=uuid.Nil;if _,err:=store.ReadWatermarkScope(context.Background(),foreign);err==nil||db.calls!=before{t.Fatal("invalid lease queried")}
 ctx,cancel:=context.WithCancel(context.Background());cancel();if _,err:=store.ReadWatermarkScope(ctx,lease);err!=context.Canceled||db.calls!=before{t.Fatal("cancel",err)}
}
