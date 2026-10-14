package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type bodyDependencyOriginStub struct{origin ExportBodyDependencyOrigin;calls int;change bool}
func(s *bodyDependencyOriginStub)LookupExportBodyDependencyOrigin(_ context.Context,_ v2input.Entry,raw []byte,_ v2input.Entry)(ExportBodyDependencyOrigin,error){
 s.calls++;raw[0]='!';out:=s.origin;if s.change&&s.calls>1{out.Producer.Fence++};return out,nil
}
func TestBodyDependencyParamsReaderPinsOriginalAndRechecks(t *testing.T){
 for _,mode:=range []string{"valid","bytes","fence","version","wrong_operation","collision","noncanonical","cancelled"}{t.Run(mode,func(t *testing.T){
  _,imageEntry,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  poses:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pose output"))}
  body,err:=canonical.Marshal(map[string]any{"image":imageEntry.Ref,"poses":poses,"person":review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1},"coefficients":map[string]any{"face_slim":0,"waist_slim":0,"hip_slim":0,"torso_slim":0,"leg_length":0}});if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)}};child:=v2input.Entry{Contract:"PoseSet",Ref:poses}
  params:=[]byte(`{}`);if mode=="noncanonical"{params=[]byte(`{ }`)}
  original:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)}
  source:=&bodyDependencyOriginStub{origin:ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Task:uuid.New(),Operation:"pose.detect",Version:"1",Attempt:1,Fence:1},Params:original,ParamsObject:objectstore.Object{Key:"params",VersionID:"immutable",SHA256:original.SHA256,Bytes:int64(len(params)),ContentType:"application/json"},Output:objectstore.Object{Key:"poses",VersionID:"immutable",SHA256:poses.SHA256,Bytes:100,ContentType:"application/json"}},change:mode=="fence"}
  switch mode{case "version":source.origin.ParamsObject.VersionID="null";case "wrong_operation":source.origin.Producer.Operation="face.landmarks";case "collision":source.origin.Params.ArtifactID=poses.ArtifactID}
  streamed:=params;if mode=="bytes"{streamed=[]byte(`[]`)}
  reader:=ExportBodyDependencyParamsReader{Source:source,Objects:statsParamsObjects{streamed}}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  origin,raw,err:=reader.ReadExportBodyDependencyParams(ctx,parent,body,child)
  if mode=="valid"{if err!=nil||origin!=source.origin||string(raw)!=string(params)||source.calls!=2{t.Fatal("original Params missing",err)}}else if err==nil||raw!=nil||origin!=(ExportBodyDependencyOrigin{}){t.Fatal("unqualified original Params returned",mode,err)}
  if rawSHA(body)!=parent.Ref.SHA256{t.Fatal("source callback mutated caller Params")}
  if mode=="cancelled"&&source.calls!=0{t.Fatal("cancelled reader reached source")}
 })}
}
