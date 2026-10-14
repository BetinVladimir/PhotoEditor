package interchange
import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)
func TestArchiveUploadIntentIdentityAndOwnedCompletion(t *testing.T){
 tid:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};request:=ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"bundle.zip",Bytes:objectstore.PartBytes+22,SHA256:strings.Repeat("a",64)}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"key",request);if err!=nil{t.Fatal(err)}
 next,err:=FreezeArchiveUpload(context.Background(),tid,auth.Identity{UserID:who.UserID,SessionID:uuid.New()},"key",request);if err!=nil||next.ID()!=intent.ID()||next.RequestSHA()!=intent.RequestSHA(){t.Fatal("rotation changed identity",err)}
 parts:=[]objectstore.UploadedPart{{PartNumber:1,ETag:"one"},{PartNumber:2,ETag:"two"}};owned,err:=intent.CompletionParts(context.Background(),request.Bytes,parts);if err!=nil{t.Fatal(err)};parts[0].ETag="changed";if owned[0].ETag!="one"{t.Fatal("borrowed parts")}
 raw:=intent.RequestBody();raw[0]='!';if intent.RequestBody()[0]=='!'{t.Fatal("borrowed request")}
 for _,invalid:=range [][]objectstore.UploadedPart{{{PartNumber:1,ETag:"one"}},{{PartNumber:2,ETag:"one"},{PartNumber:1,ETag:"two"}},{{PartNumber:1,ETag:"one"},{PartNumber:2,ETag:"bad\n"}}}{if _,err:=intent.CompletionParts(context.Background(),request.Bytes,invalid);err==nil{t.Fatal("accepted invalid coverage")}}
 for _,etag:=range []string{"bad\t","bad\x00","bad\x7f"}{if _,err:=intent.CompletionParts(context.Background(),request.Bytes,[]objectstore.UploadedPart{{PartNumber:1,ETag:"one"},{PartNumber:2,ETag:etag}});err==nil{t.Fatal("accepted control byte in ETag")}}
}
func TestArchiveUploadIntentRefusesInvalidSources(t *testing.T){
 request:=ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"bundle.zip",Bytes:22,SHA256:strings.Repeat("a",64)};tid:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 for _,mode:=range []string{"path","size","sha","project","profile"}{r:=request;switch mode{case "path":r.Filename="../bundle.zip";case "size":r.Bytes=objectstore.MaxInterchangeArchiveBytes+1;case "sha":r.SHA256=strings.Repeat("A",64);case "project":r.ProjectID=uuid.Nil;case "profile":r.Filename="photo.jpg"};if _,err:=FreezeArchiveUpload(context.Background(),tid,who,"key",r);err==nil{t.Fatal("accepted invalid source",mode)}}
 ctx,cancel:=context.WithCancel(context.Background());cancel();if _,err:=FreezeArchiveUpload(ctx,tid,who,"key",request);err!=context.Canceled{t.Fatal("ignored cancellation")}
}
