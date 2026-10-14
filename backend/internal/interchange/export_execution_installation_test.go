package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
func TestExportExecutionInstallationRefusesIncompleteDependenciesWithoutIO(t *testing.T){
 tid,who,intent,frozen,_:=exportExecutionFixture(t)
 if _,err:=(SQLExportExecutionInstaller{}).InstallExportExecution(context.Background(),who,tid,intent.Request,"pipeline-key",frozen);err!=v2input.ErrInvalid{t.Fatal("incomplete installation accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(SQLExportExecutionInstaller{}).InstallExportExecution(ctx,auth.Identity{},uuid.Nil,CreateRequest{},"key",FrozenExportDocuments{});err!=context.Canceled{t.Fatal("cancelled installation",err)}
 if err:=(SQLResolvedExportTaskMaterializer{}).MaterializeExport(context.Background(),intent,frozen,v2input.DraftTask{});err!=v2input.ErrInvalid{t.Fatal("missing materializer dependencies",err)}
 if err:=(SQLResolvedExportTaskMaterializer{}).MaterializeExport(ctx,intent,frozen,v2input.DraftTask{});err!=context.Canceled{t.Fatal("cancelled materialization",err)}
}

func TestExportExecutionInstallationRejectsSharedRolePool(t *testing.T){
 tid,who,intent,frozen,_:=exportExecutionFixture(t)
 pool:=new(pgxpool.Pool)
 installer:=SQLExportExecutionInstaller{API:pool,Scheduler:pool}
 if _,err:=installer.InstallExportExecution(context.Background(),who,tid,intent.Request,"pipeline-key",frozen);err!=v2input.ErrInvalid{t.Fatal("shared API/scheduler pool accepted",err)}
}
