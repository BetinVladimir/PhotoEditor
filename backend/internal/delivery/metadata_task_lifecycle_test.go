package delivery

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type metadataLifecycleDB struct {calls int;accepted bool;query string;cancel context.CancelFunc}
type metadataLifecycleRow struct {db *metadataLifecycleDB}
func(d *metadataLifecycleDB)QueryRow(_ context.Context,query string,args ...any)pgx.Row{d.calls++;d.query=query;return metadataLifecycleRow{d}}
func(r metadataLifecycleRow)Scan(dest ...any)error{*dest[0].(*bool)=r.db.accepted;if r.db.cancel!=nil{r.db.cancel()};return nil}
func TestMetadataLifecycleRecoveryAndBoundaries(t *testing.T){
 child:=uuid.New();workflow,_:=v2input.WorkflowID(child);task,_:=v2input.TaskID(workflow,"metadata.write",0)
 scope:=MetadataProofScope{Tenant:uuid.New(),Expansion:uuid.New(),Token:uuid.New(),ChildRun:child,Task:task}
 body,_:=canonical.Marshal(map[string]any{"id":task,"operation":"metadata.write"})
 db:=&metadataLifecycleDB{accepted:true};service:=MetadataTaskLifecycle{DB:db}
 if ready,err:=service.Activated(context.Background(),scope,body);err!=nil||!ready{t.Fatal(ready,err)}
 if db.query!=`SELECT photo.v30_metadata_task_activated($1,$2,$3,$4)`{t.Fatal("wrong recovery boundary")}
 before:=db.calls
 foreign:=scope;foreign.Task=uuid.New()
 if _,err:=service.Activated(context.Background(),foreign,body);err==nil||db.calls!=before{t.Fatal("foreign task reached SQL")}
 if err:=service.Create(context.Background(),scope,body,0);err==nil||db.calls!=before{t.Fatal("invalid quota reached SQL")}
 db.accepted=false
 if err:=service.Activate(context.Background(),scope,body);err==nil{t.Fatal("refused activation accepted")}
 ctx,cancel:=context.WithCancel(context.Background());defer cancel();db.accepted=true;db.cancel=cancel
 if _,err:=service.Activated(ctx,scope,body);err!=context.Canceled{t.Fatal("late cancellation lost",err)}
}
