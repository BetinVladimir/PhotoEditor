package interchange

import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type importNativeStyleScopeStub struct{archived,target int;deny bool;binding v2input.SessionVersionBinding}
func(s *importNativeStyleScopeStub)QualifyArchivedStyleScope(_ context.Context,raw []byte)error{s.archived++;raw[0]='x';if s.deny{return v2input.ErrInvalid};return nil}
func(s *importNativeStyleScopeStub)QualifyTargetStyleScope(_ context.Context,raw []byte,binding v2input.SessionVersionBinding)error{s.target++;s.binding=binding;raw[0]='x';if s.deny{return v2input.ErrInvalid};return nil}
func TestNativeImportStyleQualifierOwnsBytesAndRechecksScope(t *testing.T){
 root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,1000);if err!=nil{t.Fatal(err)}
 runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};scope:=&importNativeStyleScopeStub{}
 qualifier,err:=NewNativeImportStyleQualifier(scratch,probe,map[string]string{},runtime,scope);if err!=nil{t.Fatal(err)}
 raw:=[]byte(`{}`)
 if err:=qualifier.ValidateArchivedImportStyle(context.Background(),raw);err!=nil||scope.archived!=2||probe.calls!=1||string(raw)!="{}"{t.Fatal("missing native/scope checks",err)}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("master"))},CanvasHash:rawSHA([]byte("canvas"))}
 if err:=qualifier.QualifyImportedStyle(context.Background(),raw,binding);err!=nil||scope.target!=2||scope.binding!=binding||probe.calls!=2{t.Fatal("lost target authority",err)}
 scope.deny=true
 if err:=qualifier.QualifyImportedStyle(context.Background(),raw,binding);err==nil||probe.calls!=2{t.Fatal("scope refusal reached native IO")}
 if err:=qualifier.QualifyImportedStyle(context.Background(),raw,v2input.SessionVersionBinding{});err==nil||probe.calls!=2{t.Fatal("invalid binding reached probe")}
 files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",err)}
}
