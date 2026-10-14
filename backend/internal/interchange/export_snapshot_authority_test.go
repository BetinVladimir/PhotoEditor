package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
func TestExportSnapshotAuthorityRequiresInstallationAndHonoursCancellation(t *testing.T){
 s:=SQLExportSnapshotAuthority{}
 if err:=s.CheckExportSnapshot(context.Background(),auth.Identity{},uuid.New(),uuid.New(),CompiledExportSnapshot{});err==nil{t.Fatal("accepted missing authority installation")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=s.CheckExportSnapshot(ctx,auth.Identity{},uuid.New(),uuid.New(),CompiledExportSnapshot{});err!=context.Canceled{t.Fatal("ignored cancellation",err)}
}
