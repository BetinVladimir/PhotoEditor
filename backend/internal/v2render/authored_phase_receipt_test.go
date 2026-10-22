package v2render

import (
 "crypto/sha256"
 "encoding/hex"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func authoredPhaseFixture()(AuthoredInstructionSource,AuthoredPhaseTask,v2input.SessionVersionBinding,v2input.Ref,v2input.Ref){
 ref:=func(c string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat(c,64)}}
 parent:=uuid.New();previous:=uuid.New();layer:=uuid.New();workflow:=uuid.NewSHA1(parent,[]byte("workflow"));instruction,_:=v2input.TaskID(workflow,"render.authored_geometry",1)
 original:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref("a"),CanvasHash:strings.Repeat("b",64)}
 source:=AuthoredInstructionSource{Tenant:uuid.New(),Parent:parent,Workflow:workflow,Project:uuid.New(),Actor:uuid.New(),Session:uuid.New(),ProgramSHA:strings.Repeat("c",64),PredecessorSHA:strings.Repeat("d",64),OriginalRecipe:ref("e"),Source:original,Instruction:AuthoredGeometryInstruction{Identity:instruction,Ordinal:1,Kind:"geometry",Predecessor:&previous,GeometryLayer:&layer}}
 result,report:=ref("1"),ref("2");profile:="{\"backend\":\"cpu\"}";sum:=sha256.Sum256([]byte(profile))
 task:=AuthoredPhaseTask{RunID:uuid.New(),TaskID:uuid.New(),ResultID:uuid.New(),Operation:"render.authored_geometry",Version:"1",Attempt:1,Fence:1,Params:ref("3"),InputDigest:strings.Repeat("4",64),ProfileJCS:profile,ProfileSHA:hex.EncodeToString(sum[:]),Outputs:[]v2input.Ref{result,report}}
 output:=original;output.Source=ref("5");output.CanvasHash=strings.Repeat("6",64)
 return source,task,output,result,report
}
func authoredNativeWitness(task AuthoredPhaseTask)*AuthoredGeometryPhaseWitness{
 native:=task;native.RunID=uuid.New();native.TaskID=uuid.New();native.ResultID=uuid.New();native.Operation="optical.perspective"
 root:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("7",64)};image:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("8",64)};report:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("9",64)}
 native.Outputs=[]v2input.Ref{root,image,report};return &AuthoredGeometryPhaseWitness{Task:native,Root:root,Image:image,Report:report}
}
func TestCompileAuthoredPhaseReceiptActualIdentityAndDeterminism(t *testing.T){
 source,task,output,result,report:=authoredPhaseFixture();native:=authoredNativeWitness(task);raw,sha,err:=CompileAuthoredPhaseReceipt(source,task,output,result,report,native,[]v2input.Ref{result,report,output.Source});if err!=nil{t.Fatal(err)}
 again,hash,err:=CompileAuthoredPhaseReceipt(source,task,output,result,report,native,[]v2input.Ref{result,report,output.Source});if err!=nil||string(raw)!=string(again)||sha!=hash{t.Fatal("receipt is not deterministic")}
 if !strings.Contains(string(raw),task.ResultID.String())||!strings.Contains(string(raw),source.PredecessorSHA){t.Fatal("canonical occurrence omitted")}
}
func TestCompileAuthoredPhaseReceiptRefusals(t *testing.T){
 tests:=map[string]func(*AuthoredInstructionSource,*AuthoredPhaseTask,*v2input.SessionVersionBinding,*v2input.Ref){
 "foreign-version":func(s *AuthoredInstructionSource,_ *AuthoredPhaseTask,o *v2input.SessionVersionBinding,_ *v2input.Ref){o.AssetVersionID=uuid.New()},
 "parent-as-producer":func(s *AuthoredInstructionSource,x *AuthoredPhaseTask,_ *v2input.SessionVersionBinding,_ *v2input.Ref){x.RunID=s.Parent},
 "zero-attempt":func(_ *AuthoredInstructionSource,x *AuthoredPhaseTask,_ *v2input.SessionVersionBinding,_ *v2input.Ref){x.Attempt=0},
 "profile-mutation":func(_ *AuthoredInstructionSource,x *AuthoredPhaseTask,_ *v2input.SessionVersionBinding,_ *v2input.Ref){x.ProfileJCS="{}"},
 "missing-root":func(_ *AuthoredInstructionSource,x *AuthoredPhaseTask,_ *v2input.SessionVersionBinding,_ *v2input.Ref){x.Outputs=nil},
 "foreign-root":func(_ *AuthoredInstructionSource,_ *AuthoredPhaseTask,_ *v2input.SessionVersionBinding,r *v2input.Ref){r.ArtifactID=uuid.New()},
 "duplicate-root":func(_ *AuthoredInstructionSource,x *AuthoredPhaseTask,_ *v2input.SessionVersionBinding,_ *v2input.Ref){x.Outputs=append(x.Outputs,x.Outputs[0])},
 "unknown-operation":func(_ *AuthoredInstructionSource,x *AuthoredPhaseTask,_ *v2input.SessionVersionBinding,_ *v2input.Ref){x.Operation="download.encode"},
 "wrong-final-version":func(s *AuthoredInstructionSource,x *AuthoredPhaseTask,o *v2input.SessionVersionBinding,_ *v2input.Ref){s.Instruction.Kind="finalize";x.Operation="render.compose";x.Version="1";o.CanvasHash=s.Source.CanvasHash},
 "final-canvas-substitution":func(s *AuthoredInstructionSource,x *AuthoredPhaseTask,_ *v2input.SessionVersionBinding,_ *v2input.Ref){s.Instruction.Kind="finalize";x.Operation="render.compose";x.Version="2"},
 }
 for name,mutate:=range tests{t.Run(name,func(t *testing.T){s,x,o,r,report:=authoredPhaseFixture();mutate(&s,&x,&o,&r);native:=authoredNativeWitness(x);if s.Instruction.Kind!="geometry"{native=nil};if _,_,err:=CompileAuthoredPhaseReceipt(s,x,o,r,report,native,[]v2input.Ref{r,report,o.Source});err==nil{t.Fatal("invalid phase accepted")}})}
}
