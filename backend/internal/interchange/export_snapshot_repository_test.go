package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)
func TestExportSnapshotRepositoryMissingInstallationRefusesBothStages(t *testing.T){
 s:=SQLExportSnapshotObjectRepository{Bucket:"photos"};who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 if _,err:=s.ReserveExportSnapshot(context.Background(),who,uuid.New(),uuid.New(),"key",CompiledExportSnapshot{});err==nil{t.Fatal("reserved without database")}
 if _,err:=s.CompleteExportSnapshot(context.Background(),who,uuid.New(),uuid.New(),"key",CompiledExportSnapshot{},artifactjson.Reservation{},objectstore.Object{});err==nil{t.Fatal("completed without database")}
}
