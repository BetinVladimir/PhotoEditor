package provenance

import (
 "encoding/json"
 "os"
 "sort"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

func negativeGraphFixture(t *testing.T)(CanonicalSigningTask,signingNegativeCausalReceipt){
 t.Helper()
 raw,err:=os.ReadFile("../v2optical/testdata/negative-qualification.json");if err!=nil{t.Fatal(err)}
 var fixture struct{Pin v2optical.NegativeQualificationPin;Params v2optical.NegativeParams}
 if json.Unmarshal(raw,&fixture)!=nil{t.Fatal("invalid qualification fixture")}
 sourceRaw,err:=os.ReadFile("../v2optical/testdata/negative-source.json");if err!=nil{t.Fatal(err)}
 source,err:=review.ParseWorkingImage(sourceRaw);if err!=nil{t.Fatal(err)}
 params:=fixture.Params;params.Base.Source.SHA256=bodyDigest(sourceRaw);params.Base.CanvasHash,_=canonical.Hash(source.Canvas)
 _,paramsSHA,err:=params.Compile();if err!=nil{t.Fatal(err)}
 pin:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 paramsRef:=pin();paramsRef.SHA256=paramsSHA
 image:=source;image.Pixels=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)}
 _,imageRef:=contractBytes(t,image)
 reason:="negative_review"
 report:=v2draft.ImageReportDocument{SchemaVersion:1,Operation:"optical.negative",Status:"review",Reason:&reason,Sources:[]v2input.SessionVersionBinding{params.Base},Registrations:[]v2input.Ref{}}
 _,reportRef:=contractBytes(t,report)
 result:=v2draft.ImageResultDocument{SchemaVersion:1,WorkingImage:imageRef,ChangedMask:pin(),Report:reportRef}
 _,root:=contractBytes(t,result)
 outputs,_:=canonical.Marshal([]v2input.Ref{root})
 task:=CanonicalSigningTask{Task:uuid.New(),Run:uuid.New(),Result:uuid.New(),Attempt:1,Fence:1,Operation:"optical.negative",Version:"1",Params:paramsRef,Inputs:json.RawMessage(`[]`),Outputs:outputs,Metrics:json.RawMessage(`{}`),ResultDigest:strings.Repeat("c",64),ReviewRequired:true}
 arrays,_,err:=v2optical.NegativeImageInputs(params,sourceRaw,fixture.Pin.Report);if err!=nil{t.Fatal(err)}
 entries:=append([]v2input.Entry{{Ref:paramsRef,Contract:"Params"},{Ref:params.Base.Source,Contract:"WorkingImage"},{Ref:params.Calibration,Contract:"NegativeCalibration"},{Ref:fixture.Pin.Report,Contract:"QualificationReport"}},arrays...)
 sort.Slice(entries,func(i,j int)bool{return entries[i].Ref.ArtifactID.String()<entries[j].Ref.ArtifactID.String()})
 task.InputDigest,err=canonical.Hash(struct{Task uuid.UUID `json:"task"`;Operation string `json:"operation"`;Version string `json:"version"`;Entries []v2input.Entry `json:"entries"`}{task.Task,task.Operation,task.Version,entries});if err!=nil{t.Fatal(err)}
 taskSHA,err:=canonical.Hash(task);if err!=nil{t.Fatal(err)}
 evidence:=SigningNegativeEvidence{taskDigest:taskSHA,pixels:SigningNegativePixelEvidence{taskDigest:taskSHA,root:root,params:params,result:result,source:source,image:image,changedPixels:1},qualification:fixture.Pin,report:report,review:SigningNegativeReview{ID:uuid.New(),Task:task.Task,Result:task.Result,Root:root,Report:reportRef,Accepted:true}}
 evidence.stamp,err=compileSigningNegativeEvidenceStamp(task,evidence);if err!=nil{t.Fatal(err)}
 edge:=signingNegativePixelAncestry{input:params.Base.Source,producer:uuid.New()}
 stamp,err:=canonical.Hash([]any{evidence.stamp,edge.input,edge.producer,entries});if err!=nil{t.Fatal(err)}
 return task,signingNegativeCausalReceipt{evidence:evidence,pixels:edge,entries:entries,stamp:stamp}
}

func TestNegativeGraphCompilerBindsCompleteFixture(t *testing.T){
 task,receipt:=negativeGraphFixture(t)
 node,err:=compileSigningNegativeGraphTask(task,receipt);if err!=nil{t.Fatal("valid joined graph refused",err)}
 if len(node.Node.Parents)!=1||node.Node.Parents[0]!=receipt.pixels.producer||len(node.Node.Inputs)!=1||node.Node.Inputs[0]!=receipt.pixels.input||len(node.Node.Children)!=3||node.Node.Action.Generated||node.Node.Action.ReviewID==nil||*node.Node.Action.ReviewID!=receipt.evidence.review.ID{t.Fatal("graph lost exact ancestry/review outputs")}
 for _,mode:=range []string{"producer","source","inventory","stamp","review","root"}{
  t.Run(mode,func(t *testing.T){
   changed:=receipt
   switch mode{case "producer":changed.pixels.producer=task.Task;case "source":changed.pixels.input.ArtifactID=uuid.New();case "inventory":changed.entries=append([]v2input.Entry(nil),receipt.entries...);changed.entries[0].Contract="Unknown";case "stamp":changed.stamp=strings.Repeat("f",64);case "review":changed.evidence.review.Accepted=false;case "root":changed.evidence.pixels.root.ArtifactID=uuid.New()}
   if _,err:=compileSigningNegativeGraphTask(task,changed);err==nil{t.Fatal("tampered graph accepted",mode)}
  })
 }
}
