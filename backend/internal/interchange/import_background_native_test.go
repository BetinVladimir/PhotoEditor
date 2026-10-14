package interchange

import (
 "bytes"
 "context"
 "io"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type backgroundNativeObjects struct{body []byte;expected objectstore.Object;calls int}
func(s *backgroundNativeObjects)Open(_ context.Context,object objectstore.Object)(io.ReadCloser,error){s.calls++;if object!=s.expected{return nil,objectstore.ErrIntegrity};return io.NopCloser(bytes.NewReader(s.body)),nil}
func TestNativeImportedBackgroundUsesOpaqueVersionAndFullAdmission(t *testing.T){
 cases:=[]struct{name string;header []byte;media string}{
  {"png",[]byte{137,80,78,71,13,10,26,10},"image/png"},
  {"jpeg",[]byte{255,216,255,0,0,0,0,0},"image/jpeg"},
  {"tiff_le",[]byte{'I','I',42,0,0,0,0,0},"image/tiff"},
  {"tiff_be",[]byte{'M','M',0,42,0,0,0,0},"image/tiff"},
  {"bigtiff",[]byte{'I','I',43,0,8,0,0,0},"image/tiff"},
  {"unknown",[]byte("notimage"),""},
 }
 for _,item:=range cases{t.Run(item.name,func(t *testing.T){
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(item.header)}
  object:=objectstore.Object{Key:"staged",VersionID:"opaque-v1",SHA256:ref.SHA256,Bytes:int64(len(item.header)),ContentType:"application/octet-stream"}
  objects:=&backgroundNativeObjects{body:item.header,expected:object};runtime:=rawSHA([]byte("runtime"));probe:=&exportNativeProbeStub{runtime:runtime}
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,100);if err!=nil{t.Fatal(err)}
  qualifier:=NativeImportBackgroundQualifier{Native:NativeExportImageQualifier{Objects:objects,Scratch:scratch,Probe:probe,RuntimeSHA:runtime}}
  media,err:=qualifier.QualifyImportBackgroundRaster(context.Background(),ref,object)
  if item.media==""{if err==nil||probe.calls!=0{t.Fatal("unknown signature reached native probe")};return}
  if err!=nil||media!=item.media||objects.calls!=2||probe.calls!=1{t.Fatal("missing full admission or forged storage metadata",err,media)}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",err)}
 })}
}
func TestNativeImportedBackgroundMissingConfigurationDoesNoIO(t *testing.T){
 body:=[]byte{137,80,78,71,13,10,26,10};ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)}
 object:=objectstore.Object{Key:"staged",VersionID:"v1",SHA256:ref.SHA256,Bytes:8,ContentType:"application/octet-stream"}
 objects:=&backgroundNativeObjects{body:body,expected:object}
 qualifier:=NativeImportBackgroundQualifier{Native:NativeExportImageQualifier{Objects:objects}}
 if _,err:=qualifier.QualifyImportBackgroundRaster(context.Background(),ref,object);err==nil||objects.calls!=0{t.Fatal("incomplete config performed IO")}
}
