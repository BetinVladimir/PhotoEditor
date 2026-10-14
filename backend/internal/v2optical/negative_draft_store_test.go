package v2optical

import (
 "context"
 "errors"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type negativeDraftStoreDB struct{tx *negativeDraftStoreTx;begins int}
func(d *negativeDraftStoreDB)Begin(ctx context.Context)(pgx.Tx,error){
 deadline,ok:=ctx.Deadline();if !ok||time.Until(deadline)>15*time.Second{return nil,errors.New("unbounded save")}
 d.begins++;return d.tx,nil
}
type negativeDraftStoreTx struct{
 negativeSourceScopeTx
 saveMode string
 saved string
 commits,rollbacks int
}
func(t *negativeDraftStoreTx)QueryRow(ctx context.Context,q string,args ...any)pgx.Row{
 if q!="SELECT photo.save_v29_negative_draft($1,$2,$3,$4)"{return t.negativeSourceScopeTx.QueryRow(ctx,q,args...)}
 t.calls=append(t.calls,q)
 return negativeLockRow(func(dest ...any)error{
  if len(args)!=4||args[0]!=t.tenant{return errors.New("wrong saved scope")}
  t.saved=args[3].(string)
  if t.saveMode=="query_error"{return errors.New("save dependency failure")}
  *dest[0].(*bool)=t.saveMode!="refused"
  return nil
 })
}
func(t *negativeDraftStoreTx)Commit(ctx context.Context)error{
 t.commits++
 if t.saveMode=="commit_error"{return errors.New("ambiguous commit")}
 return ctx.Err()
}
func(t *negativeDraftStoreTx)Rollback(context.Context)error{t.rollbacks++;return nil}

func TestNegativeDraftStoreRepeatsLiveAuthorityAndCommitsOnlyAcceptedSave(t *testing.T){
 params,source,qualified,capacity:=negativeDraftFixture(t);body,_,_:=params.Compile()
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant,project,snapshot:=uuid.New(),uuid.New(),uuid.New()
 draft,err:=CompileNegativeDraft(context.Background(),who,tenant,project,snapshot,"negative-draft-key-0001",body,source,qualified,capacity);if err!=nil{t.Fatal(err)}
 located,err:=BindNegativeQualificationObjects(qualified,"photos",
  objectstore.Object{SHA256:qualified.pin.Report.SHA256,Bytes:int64(len(qualified.reportRaw)),ContentType:"application/json",Key:"report",VersionID:"r1"},
  objectstore.Object{SHA256:qualified.pin.Calibration.SHA256,Bytes:int64(len(qualified.calibrationRaw)),ContentType:"application/json",Key:"calibration",VersionID:"c1"});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"ok","child_stale","qualification_stale","refused","query_error","commit_error","foreign_actor","unqualified","cancelled"}{
  t.Run(mode,func(t *testing.T){
   tx:=&negativeDraftStoreTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant,mode:mode}},saveMode:mode}
   db:=&negativeDraftStoreDB{tx:tx};actor:=who;installation:=located
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   if mode=="foreign_actor"{actor.UserID=uuid.New()}
   if mode=="unqualified"{installation=LocatedNegativeQualification{}}
   if mode=="cancelled"{cancel()}
   err:=SaveNegativeDraft(ctx,db,actor,tenant,draft,installation)
   if mode=="ok"{
    if err!=nil||tx.commits!=1||tx.saved!=draft.raw{t.Fatal("valid draft not saved exactly",err)}
    if !strings.Contains(strings.Join(tx.calls,"\n"),"lock_v29_negative_qualification_objects"){t.Fatal("exact qualification locator not repeated")}
   }else if err==nil{t.Fatal("invalid/stale draft saved",mode)}
   if mode=="foreign_actor"||mode=="unqualified"||mode=="cancelled"{
    if db.begins!=0||len(tx.calls)!=0{t.Fatal("invalid local scope reached SQL")}
   }else{
    if tx.rollbacks!=1{t.Fatal("transaction cleanup missing")}
    if mode!="ok"&&mode!="commit_error"&&tx.commits!=0{t.Fatal("failed save committed")}
    if (mode=="child_stale"||mode=="qualification_stale")&&tx.saved!=""{t.Fatal("save reached before current authority")}
   }
   if mode=="refused"&&!errors.Is(err,v2input.ErrExpired){t.Fatal("false SQL acceptance not refused",err)}
  })
 }
}
