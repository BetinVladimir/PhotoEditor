package v2region
import("context";"encoding/json";"testing";"errors";"github.com/google/uuid";"github.com/jackc/pgx/v5";"photoeditor/backend/internal/canonical";"photoeditor/backend/internal/v2input")
func TestRegionRecoveryRequiresExactFrozenBodyAndConfigHash(t *testing.T){
 plan,_,_,_:=regionNodeFixture(t);doc,err:=plan.decode();if err!=nil{t.Fatal(err)}
 run,workflow:=uuid.New(),uuid.New();params:=string(plan.JSON());config:=map[string]any{"v26_region":map[string]any{"nodes":[]any{map[string]any{"params_jcs":params}}}}
 sha,err:=canonical.Hash(config);if err!=nil{t.Fatal(err)}
 ticket:=v2input.JobTicket{RunID:run,WorkflowID:workflow,State:"queued"};body,err:=json.Marshal(map[string]any{"ticket":ticket,"config":config,"config_hash":sha});if err!=nil{t.Fatal(err)}
 if got,e:=readRegionRecovery(body,doc.TenantID,doc.AssetID,run,workflow,doc.Request);e!=nil||got!=ticket{t.Fatal(got,e)}
 altered:=doc.Request;altered.MaxEdge++;for _,test:=range []struct{tenant,asset,run,workflow uuid.UUID;request RegionPreviewRequest}{{uuid.New(),doc.AssetID,run,workflow,doc.Request},{doc.TenantID,uuid.New(),run,workflow,doc.Request},{doc.TenantID,doc.AssetID,uuid.New(),workflow,doc.Request},{doc.TenantID,doc.AssetID,run,uuid.New(),doc.Request},{doc.TenantID,doc.AssetID,run,workflow,altered}}{if _,e:=readRegionRecovery(body,test.tenant,test.asset,test.run,test.workflow,test.request);e==nil{t.Fatal("foreign or changed recovery accepted")}}
 var raw map[string]any;if json.Unmarshal(body,&raw)!=nil{t.Fatal("fixture")};raw["config_hash"]="bad";bad,_:=json.Marshal(raw);if _,e:=readRegionRecovery(bad,doc.TenantID,doc.AssetID,run,workflow,doc.Request);e==nil{t.Fatal("unverified config accepted")}
}
type recoveryInstallationRow struct{value bool;err error};func(r recoveryInstallationRow)Scan(dest ...any)error{if r.err==nil{*dest[0].(*bool)=r.value};return r.err}
type recoveryInstallationDB struct{value bool;err error;calls int};func(d *recoveryInstallationDB)QueryRow(ctx context.Context,_ string,_ ...any)pgx.Row{d.calls++;if _,ok:=ctx.Deadline();!ok{return recoveryInstallationRow{err:errors.New("unbounded startup")}};return recoveryInstallationRow{d.value,d.err}}
func TestRegionRecoveryInstallationIsBoundedAndFailClosed(t *testing.T){for _,value:=range []bool{false,true}{db:=&recoveryInstallationDB{value:value};err:=RequireRecoveryInstallation(context.Background(),db);if (err==nil)!=value||db.calls!=1{t.Fatal(value,err,db.calls)}};ctx,cancel:=context.WithCancel(context.Background());cancel();db:=&recoveryInstallationDB{value:true};if !errors.Is(RequireRecoveryInstallation(ctx,db),context.Canceled)||db.calls!=0{t.Fatal("cancelled startup queried database")}}
