package delivery

import (
 "context"
 "fmt"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)

type signingStatusDB struct{value SigningChildStatus;err error;calls int}
func(d *signingStatusDB)QueryRow(context.Context,string,...any)pgx.Row{d.calls++;return signingStatusRow{d.value,d.err}}
type signingStatusRow struct{value SigningChildStatus;err error}
func(r signingStatusRow)Scan(dest ...any)error{if r.err!=nil{return r.err};*dest[0].(*string)=r.value.State;*dest[1].(*uuid.UUID)=r.value.ExportID;*dest[2].(*uuid.UUID)=r.value.TaskID;return nil}

func TestReadDeliverySignerStatusBindsParentAndTask(t *testing.T){
 lease:=ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now().Add(time.Minute),
  Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),ParentTask:uuid.New(),Cursor:1,SlotCount:1}
 actor:=uuid.New();preset:=uuid.New();key:=uuid.NewSHA1(lease.Run,[]byte(fmt.Sprintf("%d:%s",0,preset))).String()
 dependency:=SigningDependency{Ordinal:0,PresetID:preset,ExportID:uuid.New(),PolicyID:uuid.New(),IdempotencyKey:key}
 workflow:=uuid.NewSHA1(lease.Tenant,[]byte("provenance:"+actor.String()+":"+key))
 expectedTask:=uuid.NewSHA1(workflow,[]byte("provenance.sign:0"))
 for _,state:=range []string{"not_started","pending","failed","cancelled","succeeded_unverified","adopted"}{
  db:=&signingStatusDB{value:SigningChildStatus{State:state,ExportID:dependency.ExportID,TaskID:expectedTask}}
  got,err:=ReadDeliverySignerStatus(context.Background(),db,lease,actor,dependency)
  if err!=nil||got.State!=state||db.calls!=1{t.Fatal("valid status",state,err)}
 }
 for _,mode:=range []string{"foreign_export","foreign_task","unknown_state","missing","bad_key","bad_cursor"}{
  db:=&signingStatusDB{value:SigningChildStatus{State:"pending",ExportID:dependency.ExportID,TaskID:expectedTask}}
  current:=lease;dep:=dependency
  switch mode{
  case "foreign_export":db.value.ExportID=uuid.New()
  case "foreign_task":db.value.TaskID=uuid.New()
  case "unknown_state":db.value.State="ready"
  case "missing":db.err=pgx.ErrNoRows
  case "bad_key":dep.IdempotencyKey=uuid.NewString()
  case "bad_cursor":current.Cursor=0
  }
  if _,err:=ReadDeliverySignerStatus(context.Background(),db,current,actor,dep);err==nil{t.Fatal("accepted",mode)}
  if (mode=="bad_key"||mode=="bad_cursor")&&db.calls!=0{t.Fatal("invalid input reached SQL",mode)}
 }
 sha:=strings.Repeat("a",64);ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha};exportID:=dependency.ExportID
 slot:=Slot{Ordinal:0,Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref,CanvasHash:sha},
  PresetID:preset,PresetSHA:sha,Required:true,State:"ready",ExportID:&exportID,Export:&ref}
 for _,scenario:=range []struct{child,want string}{{"not_started","pending"},{"pending","pending"},
  {"succeeded_unverified","pending"},{"adopted","adopted"},{"failed","failed"},{"cancelled","failed"}}{
  db:=&signingStatusDB{value:SigningChildStatus{State:scenario.child,ExportID:dependency.ExportID,TaskID:expectedTask}}
  renews:=0
  observed,err:=ObserveDeliverySigningSet(context.Background(),db,lease,actor,dependency.PolicyID,[]Slot{slot},
   func(context.Context)error{renews++;return nil})
  if err!=nil||observed.State!=scenario.want||len(observed.Children)!=1||renews!=1||db.calls!=1{
   t.Fatal("signing set",scenario,err)
  }
 }
 db:=&signingStatusDB{value:SigningChildStatus{State:"adopted",ExportID:dependency.ExportID,TaskID:expectedTask}}
 if _,err:=ObserveDeliverySigningSet(context.Background(),db,lease,actor,dependency.PolicyID,[]Slot{slot},
  func(context.Context)error{return fmt.Errorf("lease lost")});err==nil||db.calls!=0{t.Fatal("lost lease reached signer status")}
}
