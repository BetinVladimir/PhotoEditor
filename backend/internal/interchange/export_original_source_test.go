package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestExportOriginalMediaTypesAndPreflight(t *testing.T){
 for media,want:=range map[string]string{"image/tiff":".tiff","image/jpeg":".jpg","image/png":".png"}{got,err:=exportOriginalSuffix(media);if err!=nil||got!=want{t.Fatal(media,got,err)}}
 for _,media:=range []string{"image/x-canon-cr2","application/octet-stream","image/webp",""}{if _,err:=exportOriginalSuffix(media);err==nil{t.Fatal("unsupported encoding renamed",media)}}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if got,err:=(SQLExportOriginalSource{}).Read(ctx,v2input.SessionSnapshotItem{});err!=context.Canceled||got.Ref.ArtifactID!=uuid.Nil{t.Fatal("cancelled original",err)}
 if got,err:=(SQLExportOriginalSource{}).Read(context.Background(),v2input.SessionSnapshotItem{});err!=v2input.ErrInvalid||got.Ref.ArtifactID!=uuid.Nil{t.Fatal("missing dependencies",err)}
 if _,err:=(exportOriginalCatalog{}).LookupExportDocument(context.Background(),v2input.Entry{Contract:"WorkingImage"});err!=v2input.ErrInvalid{t.Fatal("foreign role accepted",err)}
}
