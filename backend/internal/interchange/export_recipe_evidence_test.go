package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestExportRecipeObjectPhysicalBoundaries(t *testing.T){
 for _,mode:=range []string{"valid","hash","size","version","media","oversize","noncanonical","cancelled"}{t.Run(mode,func(t *testing.T){
  body:=[]byte(`{"schema_version":3}`);ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)};objects:=&importReaderStub{body:body}
  object:=objectstore.Object{Key:"recipe",VersionID:"v1",SHA256:ref.SHA256,Bytes:int64(len(body)),ContentType:"application/json"};ctx:=context.Background()
  switch mode{case "hash":objects.body=[]byte(`{"schema_version":4}`);case "size":object.Bytes++;case "version":object.VersionID="null";case "media":object.ContentType="text/html";case "oversize":object.Bytes=(1<<20)+1;case "noncanonical":objects.body=[]byte(`{ "schema_version":3}`);ref.SHA256=rawSHA(objects.body);object.SHA256=ref.SHA256;object.Bytes=int64(len(objects.body));case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  raw,err:=readExportRecipeObject(ctx,objects,ref,object)
  if mode=="valid"{if err!=nil||rawSHA(raw)!=ref.SHA256{t.Fatal("valid read",err)}}else if err==nil||raw!=nil{t.Fatal("invalid read exposed body",err)}
  if (mode=="version"||mode=="media"||mode=="oversize"||mode=="cancelled")&&objects.opens!=0{t.Fatal("invalid metadata opened object")}
 })}
}
