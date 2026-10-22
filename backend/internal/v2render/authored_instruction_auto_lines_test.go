package v2render

import (
 "bytes"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

const authoredLinesTestSHA="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func authoredCurrentLinesFixture(t *testing.T)(AuthoredGeometryMaterialization,[]byte,[]byte,[]byte,AuthoredCurrentPerspectiveLines){
 t.Helper()
 ref:=func(sha string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}}
 original:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(authoredLinesTestSHA),CanvasHash:authoredLinesTestSHA}
 current:=original;current.Source=ref(authoredLinesTestSHA)
 oldLines:=ref(authoredLinesTestSHA)
 params:=v2optical.PerspectiveParams{SchemaVersion:1,Base:original,Mode:"auto",Lines:&oldLines,Strength:.35,MaxCropFraction:.17}
 raw,sha,err:=params.Compile();if err!=nil{t.Fatal(err)}
 materialized,err:=MaterializeAuthoredGeometry("optical.perspective",ref(sha),raw,original,current);if err!=nil{t.Fatal(err)}
 selected:=AuthoredGeometryMaterialization{Instruction:AuthoredGeometryInstruction{Kind:"geometry"},Operation:"optical.perspective",Params:materialized}
 lines:=v2optical.PerspectiveLineSegments{SchemaVersion:1,Lines:[]v2optical.PerspectiveLine{
  {A:v2optical.PerspectivePoint{X:.1,Y:.1},B:v2optical.PerspectivePoint{X:.9,Y:.1},Family:"horizontal"},
  {A:v2optical.PerspectivePoint{X:.1,Y:.9},B:v2optical.PerspectivePoint{X:.9,Y:.9},Family:"horizontal"},
  {A:v2optical.PerspectivePoint{X:.1,Y:.1},B:v2optical.PerspectivePoint{X:.1,Y:.9},Family:"vertical"},
  {A:v2optical.PerspectivePoint{X:.9,Y:.1},B:v2optical.PerspectivePoint{X:.9,Y:.9},Family:"vertical"},
 }}
 segments,segmentsSHA,err:=lines.Compile();if err!=nil{t.Fatal(err)}
 detected:=v2optical.DetectedPerspectiveLines{SchemaVersion:1,Base:current,Status:"available",Lines:&lines}
 detectedRaw,err:=canonical.Marshal(detected);if err!=nil{t.Fatal(err)};detectedSHA,err:=canonical.Hash(detected);if err!=nil{t.Fatal(err)}
 profile:=`{"backend":"cpu"}`;profileSHA,err:=canonical.Hash(map[string]string{"backend":"cpu"});if err!=nil{t.Fatal(err)}
 root:=ref(detectedSHA)
 witness:=AuthoredCurrentPerspectiveLines{AuthoredLines:oldLines,CurrentLines:ref(segmentsSHA),DetectedRoot:root,
  Producer:AuthoredPhaseTask{RunID:uuid.New(),TaskID:uuid.New(),ResultID:uuid.New(),Operation:"optical.perspective_lines",Version:"1",Attempt:1,Fence:2,Params:ref(authoredLinesTestSHA),InputDigest:authoredLinesTestSHA,ProfileJCS:profile,ProfileSHA:profileSHA,Outputs:[]v2input.Ref{root}}}
 return selected,raw,detectedRaw,segments,witness
}

func TestAuthoredPerspectiveRebindsActualSegmentsPreservingAutoSettings(t *testing.T){
 selected,raw,detected,segments,witness:=authoredCurrentLinesFixture(t)
 before:=append([]byte(nil),raw...)
 out,err:=MaterializeAuthoredCurrentPerspectiveLines(selected,raw,detected,segments,witness);if err!=nil{t.Fatal(err)}
 p,err:=v2optical.ParsePerspectiveParams(out.Params.Body);if err!=nil{t.Fatal(err)}
 if p.Mode!="auto"||p.Strength!=.35||p.MaxCropFraction!=.17||p.Base!=selected.Params.Source||p.Lines==nil||*p.Lines!=witness.CurrentLines||!bytes.Equal(raw,before){t.Fatal("authored settings or original bytes changed")}
}

func TestAuthoredPerspectiveCurrentLinesSubstitutionRefused(t *testing.T){
 for _,name:=range []string{"old-lines","root-as-segments","different-source","unsupported","different-settings","fence","root-output","profile","segments-bytes"}{t.Run(name,func(t *testing.T){
  selected,raw,detected,segments,witness:=authoredCurrentLinesFixture(t)
  switch name{
  case "old-lines":witness.AuthoredLines.ArtifactID=uuid.New()
  case "root-as-segments":witness.CurrentLines=witness.DetectedRoot
  case "different-source":var p v2optical.DetectedPerspectiveLines;_ = decodeAuthoredParams(detected,&p);p.Base.Source.ArtifactID=uuid.New();detected,_=canonical.Marshal(p)
  case "unsupported":var p v2optical.DetectedPerspectiveLines;_ = decodeAuthoredParams(detected,&p);p.Status="unsupported";p.Lines=nil;detected,_=canonical.Marshal(p)
  case "different-settings":p,_:=v2optical.ParsePerspectiveParams(selected.Params.Body);p.Strength=.75;selected.Params.Body,selected.Params.SHA256,_=p.Compile()
  case "fence":witness.Producer.Fence=0
  case "root-output":witness.Producer.Outputs=[]v2input.Ref{witness.CurrentLines}
  case "profile":witness.Producer.ProfileJCS="{}"
  case "segments-bytes":segments=append(segments,' ')
  }
  if _,err:=MaterializeAuthoredCurrentPerspectiveLines(selected,raw,detected,segments,witness);err==nil{t.Fatal("substitution accepted")}
 })}
}
