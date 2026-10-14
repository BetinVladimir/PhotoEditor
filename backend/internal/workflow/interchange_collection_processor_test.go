package workflow
import("context";"crypto/sha256";"encoding/hex";"io";"strings";"testing";"github.com/google/uuid";"photoeditor/backend/internal/objectstore")
type interchangeCollectionPorts struct{candidate *InterchangeCollectionCandidate;body string;opened,prepared bool}
func(p *interchangeCollectionPorts)Next(context.Context)(*InterchangeCollectionCandidate,error){return p.candidate,nil}
func(p *interchangeCollectionPorts)Prepare(_ context.Context,_ InterchangeCollectionCandidate,raw []byte)(bool,error){p.prepared=true;return string(raw)==p.body,nil}
func(p *interchangeCollectionPorts)Open(context.Context,objectstore.Object)(io.ReadCloser,error){p.opened=true;return io.NopCloser(strings.NewReader(p.body)),nil}
func TestInterchangeCollectionProcessorRequiresExactPinnedBytes(t *testing.T){
 for _,mode:=range []string{"valid","hash","size","bucket","cancelled","idle"}{t.Run(mode,func(t *testing.T){
  body:=`{"schema_version":1}`;sum:=sha256.Sum256([]byte(body));c:=&InterchangeCollectionCandidate{Tenant:uuid.New(),Workflow:uuid.New(),Artifact:uuid.New(),SHA:hex.EncodeToString(sum[:]),Bytes:int64(len(body)),Bucket:"photos",Key:"pinned.json",Version:"version"}
  p:=&interchangeCollectionPorts{candidate:c,body:body};ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{case "hash":c.SHA=strings.Repeat("a",64);case "size":c.Bytes++;case "bucket":c.Bucket="other";case "cancelled":cancel();case "idle":p.candidate=nil}
  done,err:=(InterchangeCollectionProcessor{Store:p,Objects:p,Bucket:"photos"}).ProcessOne(ctx)
  if mode=="valid"{if err!=nil||!done||!p.prepared{t.Fatal("collection not prepared",err)};return}
  if done||p.prepared{t.Fatal("invalid source prepared collection")}
  if mode=="idle"{if err!=nil||p.opened{t.Fatal("idle performed IO",err)}}else if err==nil{t.Fatal("invalid source accepted")}
  if (mode=="bucket"||mode=="cancelled")&&p.opened{t.Fatal("denied source opened")}
 })}
}
