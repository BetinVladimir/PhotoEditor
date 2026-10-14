package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type lutAuthorityStub struct {calls int;deny bool}
func(s *lutAuthorityStub)QualifyExportLUT(_ context.Context,_ v2input.Entry,raw []byte,_ v2input.Entry)error {
 s.calls++;raw[0]='!';if s.deny{return v2input.ErrInvalid};return nil
}
func TestLUTGeometryRequiresExactTypedEdgeAndCurrentAuthority(t *testing.T) {
 for _,mode:=range []string{"valid","missing","deny","hash","edge","grid","dtype","cancelled"} {t.Run(mode,func(t *testing.T){
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("lut"))},Contract:"AbsoluteLUTArray"}
  size:=33;dtype:="float32";if mode=="grid"{size=34};if mode=="dtype"{dtype="uint16"}
  raw,err:=canonical.Marshal(map[string]any{"absolute_lut":map[string]any{"artifact":entry.Ref,"size":size,"dtype":dtype,"order":"RGB_C","profile_id":"work_rec2020_gamma24_v1"}});if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"ResolvedStyle"}
  reader:=&exportMaskNodeStub{node:ExportClosureNode{Entry:parent,Body:raw,Children:[]v2input.Entry{entry}}}
  authority:=&lutAuthorityStub{deny:mode=="deny"}
  var installed ExportLUTOccurrenceAuthority=authority;if mode=="missing"{installed=nil}
  source,err:=NewExportTypedLUTGeometrySource(reader,[]v2input.Entry{parent},installed)
  if mode=="missing"{if err==nil{t.Fatal("missing authority accepted")};return};if err!=nil{t.Fatal(err)}
  if mode=="hash"{reader.node.Body=[]byte("{}")};if mode=="edge"{reader.node.Children=nil}
  ctx:=context.Background();if mode=="cancelled"{var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  geometry,err:=source.ResolveExportArrayGeometry(ctx,entry)
  if mode=="valid" {if err!=nil||geometry.Width!=33||geometry.Height!=33||geometry.DType!="float32"||authority.calls!=1||rawSHA(raw)!=parent.Ref.SHA256 {t.Fatal(geometry,err)}} else if err==nil{t.Fatal("invalid LUT geometry accepted")}
 })}
}

func TestLUTRouterNeverFallsBackToOrdinaryArrays(t *testing.T) {
 lut,ordinary:=&exportBinaryQualifierStub{},&exportBinaryQualifierStub{}
 entry:=v2input.Entry{Contract:"AbsoluteLUTArray"}
 if !exportBinaryContract(entry.Contract){t.Fatal("LUT not binary")}
 router:=ExportNativeQualifierRouter{LUTs:lut,Arrays:ordinary}
 if err:=router.QualifyExportBinary(context.Background(),entry,objectstore.Object{});err!=nil||lut.calls!=1||ordinary.calls!=0{t.Fatal("incorrect LUT route",err)}
 router.LUTs=nil
 if err:=router.QualifyExportBinary(context.Background(),entry,objectstore.Object{});err==nil||ordinary.calls!=0{t.Fatal("LUT authority bypassed")}
}
