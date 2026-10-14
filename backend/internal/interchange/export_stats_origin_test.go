package interchange
import (
 "context"
 "bytes"
 "io"
 "photoeditor/backend/internal/objectstore"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type statsOriginStub struct{calls int;evidence ExportStatsOriginEvidence}
func(s *statsOriginStub)LookupExportStatsOrigin(context.Context,v2input.Entry,[]byte)(ExportStatsOriginEvidence,error){s.calls++;return s.evidence,nil}
type statsPersonStub struct{}
func(statsPersonStub)QualifyExportStatsPerson(context.Context,v2input.Entry,v2input.Ref,[]byte,review.StatsParams,review.WorkingImage)error{return v2input.ErrInvalid}
func TestStatsOriginRejectsMissingOrInvalidOriginalParamsBeforeIO(t *testing.T){
 raw,err:=canonical.Marshal(map[string]any{"source_image":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("source"))},"feature_version":"stats20_v1","values":make([]float64,20),"coverage_bins":make([]bool,512),"mask_confidence":1,"warnings":[]string{}});if err!=nil{t.Fatal(err)}
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"Stats20"}
 for _,mode:=range []string{"metadata","origin","person","images","cancelled"}{t.Run(mode,func(t *testing.T){
  origin:=&statsOriginStub{}
  policy:=ExportStatsOriginQualifier{Origin:origin,Objects:exportDefectNoIO{t},Images:&exportMaskNodeStub{},Person:statsPersonStub{}}
  ctx:=context.Background()
  switch mode{case "origin":policy.Origin=nil;case "person":policy.Person=nil;case "images":policy.Images=nil;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  if err:=policy.QualifyExportStats(ctx,entry,raw);err==nil{t.Fatal("unqualified original Params accepted")}
  expected:=0;if mode=="metadata"{expected=1};if origin.calls!=expected{t.Fatal("unexpected origin reads",origin.calls)}
 })}
}

type statsParamsObjects struct{raw []byte}
func(s statsParamsObjects)Open(context.Context,objectstore.Object)(io.ReadCloser,error){return io.NopCloser(bytes.NewReader(s.raw)),nil}
type statsOriginSequence struct{evidence ExportStatsOriginEvidence;calls int;change bool}
func(s *statsOriginSequence)LookupExportStatsOrigin(context.Context,v2input.Entry,[]byte)(ExportStatsOriginEvidence,error){s.calls++;out:=s.evidence;if s.change&&s.calls>1{out.Object.VersionID="changed"};return out,nil}
type statsPersonPolicy struct{calls int;deny bool}
func(s *statsPersonPolicy)QualifyExportStatsPerson(context.Context,v2input.Entry,v2input.Ref,[]byte,review.StatsParams,review.WorkingImage)error{s.calls++;if s.deny{return v2input.ErrInvalid};return nil}
func TestStatsOriginalParamsPhysicalBindingAndFinalRecheck(t *testing.T){
 for _,mode:=range []string{"valid","changed","person-deny","confidence","params-hash"}{t.Run(mode,func(t *testing.T){
  _,_,imageRaw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  imageEntry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(imageRaw)},Contract:"WorkingImage"}
  paramsRaw,err:=canonical.Marshal(review.StatsParams{Image:review.ArtifactRef{ID:imageEntry.Ref.ArtifactID,SHA256:imageEntry.Ref.SHA256},Person:review.MaskRef{
   Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("person"))},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:.8}});if err!=nil{t.Fatal(err)}
  confidence:=.8;if mode=="confidence"{confidence=.5}
  statsRaw,err:=canonical.Marshal(map[string]any{"source_image":imageEntry.Ref,"feature_version":"stats20_v1","values":make([]float64,20),"coverage_bins":make([]bool,512),"mask_confidence":confidence,"warnings":[]string{}});if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(statsRaw)},Contract:"Stats20"}
  evidence:=ExportStatsOriginEvidence{Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(paramsRaw)},Object:objectstore.Object{Key:"params",VersionID:"immutable",SHA256:rawSHA(paramsRaw),Bytes:int64(len(paramsRaw)),ContentType:"application/json"}}
  origin:=&statsOriginSequence{evidence:evidence,change:mode=="changed"};person:=&statsPersonPolicy{deny:mode=="person-deny"}
  streamed:=append([]byte(nil),paramsRaw...);if mode=="params-hash"{streamed[0]='!'}
  policy:=ExportStatsOriginQualifier{Origin:origin,Objects:statsParamsObjects{streamed},Images:&exportMaskNodeStub{node:ExportClosureNode{Entry:imageEntry,Body:imageRaw}},Person:person}
  err=policy.QualifyExportStats(context.Background(),entry,statsRaw)
  if mode=="valid"{if err!=nil||origin.calls!=2||person.calls!=1{t.Fatal("incomplete origin path",err)}}else if err==nil{t.Fatal("unqualified origin accepted")}
  if (mode=="confidence"||mode=="params-hash")&&person.calls!=0{t.Fatal("invalid bytes/source reached person authority")}
 })}
}
