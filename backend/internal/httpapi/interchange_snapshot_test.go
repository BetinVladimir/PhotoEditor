package httpapi
import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/interchange"
 "photoeditor/backend/internal/v2input"
)
type snapshotHTTPStub struct{calls int;corrupt bool}
func(s *snapshotHTTPStub)Prepare(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,key string,raw []byte)(interchange.ExportSnapshotReceipt,error){
 s.calls++;compiled,err:=interchange.CompileExportSnapshotRequest(ctx,tid,pid,who,key,raw);if err!=nil{return interchange.ExportSnapshotReceipt{},err}
 var snapshot v2input.SessionInputSnapshot
 if err:=json.Unmarshal(compiled.Body(),&snapshot);err!=nil{return interchange.ExportSnapshotReceipt{},err}
 root:=v2input.Ref{ArtifactID:uuid.NewSHA1(compiled.ID(),[]byte("interchange:export-snapshot:artifact:1")),SHA256:snapshotHTTPTestSHA(compiled.Body())}
 receipt:=interchange.ExportSnapshotReceipt{ID:compiled.ID(),Artifact:root,Digest:snapshot.SnapshotDigest,ItemCount:len(snapshot.Items)}
 if s.corrupt{receipt.Artifact.ArtifactID=uuid.New()};return receipt,nil
}
func TestInterchangeSnapshotHTTPBindsReceiptAndRejectsInvalidBeforeService(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid,pid:=uuid.New(),uuid.New()
 source:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 item:=v2input.SessionSnapshotItem{Ordinal:0,Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:source,CanvasHash:strings.Repeat("b",64)},WorkingImage:source,Recipe:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("c",64)}}
 raw,err:=canonical.Marshal(interchange.ExportSnapshotRequest{Items:[]v2input.SessionSnapshotItem{item}});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"ok","corrupt","invalid","missing-key","uninstalled"}{t.Run(mode,func(t *testing.T){
  stub:=&snapshotHTTPStub{corrupt:mode=="corrupt"};var service InterchangeSnapshotPreparer=stub
  body,key,status:=string(raw),"key",201
  switch mode{case "corrupt":status=503;case "invalid":body=`{"items":[]}`;status=422;case "missing-key":key="";status=422;case "uninstalled":service=nil;status=503}
  r:=httptest.NewRequest("POST","/",strings.NewReader(body));r.Header.Set("Idempotency-Key",key);w:=httptest.NewRecorder()
  serveInterchangeSnapshot(w,r,who,tid,pid,service)
  if w.Code!=status{t.Fatalf("status=%d body=%s",w.Code,w.Body.String())}
  if status==422&&stub.calls!=0{t.Fatal("invalid request reached preparation")}
 })}
}

func snapshotHTTPTestSHA(raw []byte)string{sum:=sha256.Sum256(raw);return hex.EncodeToString(sum[:])}
