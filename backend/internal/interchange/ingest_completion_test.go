package interchange
import("context";"strings";"testing";"github.com/google/uuid";"github.com/jackc/pgx/v5";"photoeditor/backend/internal/review";"photoeditor/backend/internal/workflow")
type finishRow struct{state *string}
func(r finishRow)Scan(dest ...any)error{*dest[0].(**string)=r.state;return nil}
type finishTx struct{state *string;args []any}
func(t *finishTx)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{t.args=args;return finishRow{t.state}}
func TestImportedRejectBindsTerminalEvidence(t *testing.T){
 tenant,who,_,_,_:=pendingFixture();run:=uuid.New();tx:=&proofTx{accepted:true};repo:=ImportedCatalogAdopter{DB:tx,Who:who}
 failure:=workflow.IngestItemFailure{Ordinal:3,Source:review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)},Stage:"asset.decode",Reason:"failed"}
 ok,err:=repo.RejectFile(context.Background(),tenant,run,failure);if err!=nil||!ok||len(tx.args)!=9||tx.args[1]!=who.UserID||tx.args[3]!=run||tx.args[5]!=failure.Source.ID||tx.args[8]!=failure.Reason{t.Fatal("rejection scope changed",err)}
 for _,reason:=range []string{"running","retry_wait","succeeded",""}{tx.args=nil;failure.Reason=reason;if _,err:=repo.RejectFile(context.Background(),tenant,run,failure);err==nil||tx.args!=nil{t.Fatal("nonterminal reason queried",reason)}}
}
func TestImportedFinishPreservesPendingAndKnownTerminalStates(t *testing.T){
 tenant,who,_,_,_:=pendingFixture();run:=uuid.New()
 for _,state:=range []string{"","completed","completed_with_errors","failed","running","cancelled"}{t.Run(state,func(t *testing.T){
  tx:=&finishTx{};if state!=""{tx.state=&state};repo:=ImportedCatalogAdopter{DB:tx,Who:who}
  got,err:=repo.Finish(context.Background(),tenant,run)
  if state=="running"||state=="cancelled"{if err==nil||got!=""{t.Fatal("invalid terminal accepted")};return}
  if err!=nil||got!=state||len(tx.args)!=4||tx.args[0]!=tenant||tx.args[1]!=who.UserID||tx.args[2]!=who.SessionID||tx.args[3]!=run{t.Fatal("finish scope/state changed",err)}
 })}
}
