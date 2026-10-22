package v2portrait

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Adapter scenarios use compiled receipt fixtures and an explicit SQL stub.
// They do not establish PostgreSQL qualification/source authority or GPU proof.
type storedAnalysisRouterDB struct {body []byte;queryArgs []any;reads,commits int;commitErr error}
type storedAnalysisRouterTx struct {pgx.Tx;db *storedAnalysisRouterDB}
func (d *storedAnalysisRouterDB) Begin(context.Context)(pgx.Tx,error){return &storedAnalysisRouterTx{db:d},nil}
func (tx *storedAnalysisRouterTx) Commit(context.Context)error{tx.db.commits++;return tx.db.commitErr}
func (tx *storedAnalysisRouterTx) Rollback(context.Context)error{return nil}
func (tx *storedAnalysisRouterTx) Exec(_ context.Context,query string,_ ...any)(pgconn.CommandTag,error){
 if query!="SELECT set_config('app.tenant_id',$1,true)" {panic("unexpected mutation: "+query)}
 return pgconn.NewCommandTag("SELECT 1"),nil
}
func (tx *storedAnalysisRouterTx) QueryRow(_ context.Context,query string,args ...any)pgx.Row{
 if query=="SELECT current_user" {return donorSQLRow(func(dest ...any)error{*dest[0].(*string)="photo_scheduler";return nil})}
 if !strings.HasPrefix(query,"SELECT photo.read_v28_relight_analysis_stored_outcome("){panic("unexpected query: "+query)}
 tx.db.reads++;tx.db.queryArgs=append([]any(nil),args...)
 return donorSQLRow(func(dest ...any)error{*dest[0].(*[]byte)=append([]byte(nil),tx.db.body...);return nil})
}
func TestStoredAnalysisProductionRouterRecovery(t *testing.T){
 for _,mode:=range []string{"removed","replaced","absent","denied","conflict","commit_failure","malformed"}{t.Run(mode,func(t *testing.T){
  request,who,fixture:=analysisSubmitFixture(t)
  intent:=RelightAnalysisIntent{SchemaVersion:1,Snapshot:request.Snapshot,Base:request.Base,Operation:request.Operation}
  envelope:=map[string]any{"outcome":"receipt","receipt":fixture.receipt}
  if mode=="absent"||mode=="denied"||mode=="conflict" {envelope=map[string]any{"outcome":mode}}
  if mode=="malformed" {envelope["receipt"]=map[string]any{"state":"queued"}}
  raw,err:=canonical.Marshal(envelope);if err!=nil{t.Fatal(err)}
  db:=&storedAnalysisRouterDB{body:raw};router,_:=NewRelightAnalysisReceiptOnlyProjects(db)
  if mode=="replaced" {router.services[analysisProjectKey{request.Tenant,request.Project,request.Operation}]=&RelightAnalysisService{}}
  if mode=="commit_failure" {db.commitErr=errors.New("commit acknowledgement lost")}
  receipt,err:=router.SubmitProjectAnalysis(WithRelightReplayOnly(context.Background()),who,request.Tenant,request.Project,request.Key,intent)
  if mode=="removed"||mode=="replaced" {
   if err!=nil||receipt.Run!=fixture.receipt.Run||string(receipt.Transport)!=string(fixture.receipt.Transport)||db.commits!=1{t.Fatal(receipt,err)}
  }else{
   if err==nil||receipt.Run!=(RelightAnalysisReceipt{}).Run{t.Fatal("unverified receipt escaped",receipt,err)}
   if mode=="absent"&&!errors.Is(err,v2input.ErrExpired){t.Fatal(err)}
   if mode=="conflict"&&!errors.Is(err,v2input.ErrConflict){t.Fatal(err)}
   if mode=="commit_failure"&&!errors.Is(err,db.commitErr){t.Fatal(err)}
   if mode!="commit_failure"&&db.commits!=0{t.Fatal("refused receipt committed")}
  }
  if db.reads!=1||len(db.queryArgs)!=6 {t.Fatal("unexpected admission/repeated read",db.reads)}
  var submitted RelightAnalysisIntent
  if json.Unmarshal([]byte(db.queryArgs[5].(string)),&submitted)!=nil||submitted!=intent {t.Fatal("stored read changed original intent")}
 })}
}
