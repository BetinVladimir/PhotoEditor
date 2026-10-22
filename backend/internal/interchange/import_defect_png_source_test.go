package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestArchivedDefectPNGSourceRequiresIndependentAuthorities(t *testing.T){
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("png"))},Contract:"DefectLabelPNG"}
 source:=ImportArchivedDefectPNGSource{}
 if result,err:=source.ResolveExportDefectPNG(context.Background(),entry);err==nil||result!=nil{t.Fatal("uninstalled archive source accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if result,err:=source.ResolveExportDefectPNG(ctx,entry);!errors.Is(err,context.Canceled)||result!=nil{t.Fatal("cancellation lost",err)}
}
func TestEntryPNGDispatchDoesNotReuseAnotherEntry(t *testing.T){
 router:=importEntryDefectPNGRouter{entries:map[uuid.UUID]ImportDefectPNGBinaryRemapper{uuid.New():{}}}
 result,size,err:=router.RemapImportBinary(context.Background(),uuid.New(),v2input.Ref{},v2input.Ref{},"DefectLabelPNG",v2input.SessionVersionBinding{},v2input.SessionVersionBinding{},100)
 if err==nil||result!=(v2input.Ref{})||size!=0{t.Fatal("foreign entry authority reused")}
}
