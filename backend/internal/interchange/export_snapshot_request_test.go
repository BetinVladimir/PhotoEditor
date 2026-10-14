package interchange
import (
 "context"
 "bytes"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestExportSnapshotRequestPinsRecipeAndOwnsBytes(t *testing.T){
 _,project,ref,raw,_:=exportPlanFixture(t)
 snapshot,err:=v2input.DecodePinnedSessionInputSnapshot(ref,raw);if err!=nil{t.Fatal(err)}
 body,err:=canonical.Marshal(ExportSnapshotRequest{Items:snapshot.Items});if err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant:=uuid.New()
 compiled,err:=CompileExportSnapshotRequest(context.Background(),tenant,project,who,"key",body);if err!=nil{t.Fatal(err)}
 repeat,err:=CompileExportSnapshotRequest(context.Background(),tenant,project,auth.Identity{UserID:who.UserID,SessionID:uuid.New()},"key",body);if err!=nil{t.Fatal(err)}
 if repeat.ID()!=compiled.ID()||!bytes.Equal(repeat.Body(),compiled.Body()){t.Fatal("session rotation changed frozen identity")}
 result,err:=v2input.DecodePinnedSessionInputSnapshot(v2input.Ref{ArtifactID:compiled.ID(),SHA256:rawSHA(compiled.Body())},compiled.Body());if err!=nil||result.Items[0].Recipe!=snapshot.Items[0].Recipe{t.Fatal("recipe lost",err)}
 body[0]='!';copyBody:=compiled.Body();copyBody[0]='!';copyRequest:=compiled.Request();copyRequest[0]='!'
 if compiled.Body()[0]=='!'||compiled.Request()[0]=='!'{t.Fatal("borrowed result bytes")}
}
func TestExportSnapshotRequestRejectsUnpinnedAndForeignFields(t *testing.T){
 tenant,project:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 for _,raw:=range []string{`{}`,`{"items":[]}`,`{"items":[],"snapshot_id":"foreign"}`,`{"items":null}`,`{"items":[{"ordinal":0}]}`} {
  if _,err:=CompileExportSnapshotRequest(context.Background(),tenant,project,who,"key",[]byte(raw));err==nil{t.Fatal("accepted invalid request",raw)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=CompileExportSnapshotRequest(ctx,tenant,project,who,"key",[]byte(`{}`));err!=context.Canceled{t.Fatal("ignored cancellation",err)}
}
