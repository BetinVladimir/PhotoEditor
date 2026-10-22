package v2render

import (
 "encoding/json"
 "testing"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// These are content-projection controls, not SQL or physical adoption proofs.
func authoredFinalBaselineFixture(t *testing.T)(AuthoredPhaseReceipt,authoredFinalCompoundGeometry){
 t.Helper()
 source,task,output,result,report:=authoredPhaseFixture()
 native:=authoredNativeWitness(task)
 c:=authoredFinalCompoundGeometry{Registered:result}
 c.Blend.Baseline=report;c.Blend.Proposal=native.Image
 c.Blend.Sources=[]v2input.SessionVersionBinding{source.Source}
 c.Blend.Reference=0;c.Blend.ReferenceSHA=source.Source.Source.SHA256
 c.Blend.Target=review.Canvas{Width:300,Height:200,Orientation:1,SpaceID:"target",SourceSHA256:c.Blend.ReferenceSHA}
 c.Blend.Chain=json.RawMessage(`{"schema_version":1,"steps":[]}`)
 c.Blend.Color=review.ColorSpace{ProfileID:"test",ICCSHA256:c.Blend.ReferenceSHA,Encoding:"linear"}
 var err error;output.CanvasHash,err=canonical.Hash(c.Blend.Target);if err!=nil{t.Fatal(err)}
 return AuthoredPhaseReceipt{InputSource:source.Source,OutputSource:output,Geometry:native,PhysicalClosure:[]v2input.Ref{result,report}},c
}
func TestAuthoredFinalCanonicalBaselineAndRegisteredGeometry(t *testing.T){
 phase,c:=authoredFinalBaselineFixture(t)
 for _,ref:=range []v2input.Ref{c.Blend.Baseline,c.Registered}{
 raw,_:=canonical.Marshal(c);g,color,found,err:=authoredFinalRegisteredGeometry(phase,raw,ref)
 chain,_:=canonical.Hash(c.Blend.Chain)
 if err!=nil||!found||g.CanvasHash!=phase.OutputSource.CanvasHash||g.TransformHash!=chain||color!=c.Blend.Color{t.Fatal("canonical baseline projection refused",err)}
 }
}
func TestAuthoredFinalBaselineSubstitutionsRefused(t *testing.T){
 for _,name:=range []string{"not-retained","proposal","reference-negative","reference-overflow","source","canvas","container","no-native","ref-sha"}{t.Run(name,func(t *testing.T){
 phase,c:=authoredFinalBaselineFixture(t);ref:=c.Blend.Baseline
 switch name{
 case "not-retained":phase.PhysicalClosure=nil
 case "proposal":c.Blend.Proposal=c.Registered
 case "reference-negative":c.Blend.Reference=-1
 case "reference-overflow":c.Blend.Reference=len(c.Blend.Sources)
 case "source":c.Blend.Sources[0].Source=c.Registered
 case "canvas":c.Blend.Target.Width++
 case "container":c.Blend.ReferenceSHA="different"
 case "no-native":phase.Geometry=nil
 case "ref-sha":ref.SHA256="bad";c.Blend.Baseline=ref;phase.PhysicalClosure=[]v2input.Ref{ref}
 }
 raw,_:=canonical.Marshal(c);if _,_,_,err:=authoredFinalRegisteredGeometry(phase,raw,ref);err==nil{t.Fatal("substitution accepted")}
 })}
}
func TestAuthoredFinalOpaqueRetainedLeafIsNotGeometry(t *testing.T){
 phase,c:=authoredFinalBaselineFixture(t);ref:=phase.Geometry.Report;phase.PhysicalClosure=append(phase.PhysicalClosure,ref)
 raw,_:=canonical.Marshal(c);if _,_,found,err:=authoredFinalRegisteredGeometry(phase,raw,ref);err!=nil||found{t.Fatal("opaque leaf acquired geometry")}
}
