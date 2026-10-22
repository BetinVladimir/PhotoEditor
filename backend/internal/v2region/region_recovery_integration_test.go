package v2region
import("context";"encoding/json";"os";"testing";"time";"github.com/google/uuid";"github.com/jackc/pgx/v5/pgxpool";"photoeditor/backend/internal/auth")
// Use the relational accepted fixture, never a fabricated successful renderer.
// No Preparation/Profiles/Scheduler/objects are installed in this recovering API.
func TestIntegrationRegionQueueRecoversAcceptedWithoutFreshProviders(t *testing.T){
 raw:=os.Getenv("PHOTO_REGION_RECOVERY_CASE");if raw==""{t.Skip("isolated accepted Region SQL fixture required")}
 var f struct{Tenant,Asset,Actor,Session,Run,Workflow uuid.UUID;Key string;Request RegionPreviewRequest}
 if err:=json.Unmarshal([]byte(raw),&f);err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 cfg,err:=pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"));if err!=nil{t.Fatal(err)};cfg.ConnConfig.RuntimeParams["role"]="photo_api"
 pool,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer pool.Close()
 if err=RequireRecoveryInstallation(ctx,pool);err!=nil{t.Fatal(err)}
 queue:=QueueService{DB:pool}
 who:=auth.Identity{UserID:f.Actor,SessionID:f.Session}
 for i:=0;i<2;i++{got,e:=queue.Queue(ctx,who,f.Tenant,f.Asset,f.Key,f.Request);if e!=nil||got.RunID!=f.Run||got.WorkflowID!=f.Workflow||got.State!="queued"{t.Fatal("accepted recovery required fresh dependencies",got,e)}}
 changed:=f.Request;changed.MaxEdge++;if _,e:=queue.Queue(ctx,who,f.Tenant,f.Asset,f.Key,changed);e==nil{t.Fatal("changed body replayed")}
 if _,e:=queue.Queue(ctx,who,f.Tenant,f.Asset,uuid.NewString(),f.Request);e==nil{t.Fatal("new work admitted without fresh providers")}
}
