package v2render

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// These are pure content-compiler controls, not SQL publication/qualification.
func authoredContinuationFixture(t *testing.T) AuthoredFinalDocuments {
 t.Helper();source,task,output,result,report:=authoredPhaseFixture();native:=authoredNativeWitness(task)
 bodies:=map[v2input.Ref][]byte{}
 document:=func(id uuid.UUID,value any)v2input.Ref{raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)};sum:=sha256.Sum256(raw);ref:=v2input.Ref{ArtifactID:id,SHA256:hex.EncodeToString(sum[:])};bodies[ref]=raw;return ref}
 base:=document(uuid.New(),map[string]any{"tenant_id":source.Tenant,"parent_run_id":source.Parent,"original_recipe":source.OriginalRecipe,"original_source":source.Source,"processed_source":source.Source})
 source.PredecessorSHA=base.SHA256;registered:=v2input.Ref{ArtifactID:uuid.New(),SHA256:source.Source.Source.SHA256}
 compound:=map[string]any{"schema_version":1,"operation":"render.authored_geometry","operation_version":"1","tenant_id":source.Tenant,"parent_run_id":source.Parent,"instruction_id":source.Instruction.Identity,"program_sha256":source.ProgramSHA,"recipe":source.OriginalRecipe,"original_source":source.Source,"input_source":source.Source,"registered_original":registered,"blended_image":output.Source,"native":map[string]any{"run_id":native.Task.RunID,"task_id":native.Task.TaskID,"result_id":native.Task.ResultID,"attempt":native.Task.Attempt,"fence":native.Task.Fence,"operation":native.Task.Operation,"operation_version":native.Task.Version,"params":native.Task.Params,"root":native.Root,"image":native.Image,"report":native.Report}}
 result=document(result.ArtifactID,compound);task.Outputs[0]=result
 raw,sha,err:=CompileAuthoredPhaseReceipt(source,task,output,result,report,native,[]v2input.Ref{result,report,output.Source,registered});if err!=nil{t.Fatal(err)}
 phaseRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha};bodies[phaseRef]=raw
 phase:=AuthoredPublishedPhase{Ordinal:1,Kind:"geometry",Instruction:source.Instruction.Identity,Receipt:phaseRef,ReceiptJCS:string(raw)}
 source.Source=output;source.PredecessorSHA=sha;source.Instruction.Kind="finalize";source.Instruction.Ordinal=2;source.Instruction.Identity=uuid.New()
 return AuthoredFinalDocuments{Inventory:AuthoredFinalInventory{Context:source,BaseReceipt:base,BaseReceiptJCS:string(bodies[base]),Phases:[]AuthoredPublishedPhase{phase}},Bodies:bodies}
}
func TestAuthoredContinuationCanonicalActualOccurrences(t *testing.T){
 fixture:=authoredContinuationFixture(t);body,sha,registered,err:=CompileAuthoredFinalContinuation(fixture);if err!=nil{t.Fatal(err)}
 var wire AuthoredCompoundContinuation;if json.Unmarshal(body,&wire)!=nil||wire.SchemaVersion!=2||len(wire.Stages)!=1||wire.Stages[0].CanonicalReceipt!=fixture.Inventory.Phases[0].Receipt||registered.ArtifactID==uuid.Nil{t.Fatal("actual occurrence lost")}
 sum:=sha256.Sum256(body);if sha!=hex.EncodeToString(sum[:]){t.Fatal("digest mismatch")}
}
func TestAuthoredContinuationRejectsAlteredTranscript(t *testing.T){
 tests:=map[string]func(*AuthoredFinalDocuments){
  "missing-object":func(d *AuthoredFinalDocuments){delete(d.Bodies,d.Inventory.BaseReceipt)},
  "changed-object-bytes":func(d *AuthoredFinalDocuments){d.Bodies[d.Inventory.BaseReceipt]=[]byte("{}")},
  "foreign-parent":func(d *AuthoredFinalDocuments){d.Inventory.Context.Parent=uuid.New()},
  "missing-phase":func(d *AuthoredFinalDocuments){d.Inventory.Phases=nil},
  "wrong-ordinal":func(d *AuthoredFinalDocuments){d.Inventory.Phases[0].Ordinal=2},
  "wrong-current":func(d *AuthoredFinalDocuments){d.Inventory.Context.Source.Source.ArtifactID=uuid.New()},
  "wrong-predecessor":func(d *AuthoredFinalDocuments){d.Inventory.Context.PredecessorSHA=d.Inventory.BaseReceipt.SHA256},
 }
 for name,mutate:=range tests{t.Run(name,func(t *testing.T){fixture:=authoredContinuationFixture(t);mutate(&fixture);if _,_,_,err:=CompileAuthoredFinalContinuation(fixture);err==nil{t.Fatal("altered transcript accepted")}})}
}

func TestAuthoredFinalMaterializationUsesActualPublishedReference(t *testing.T){
 fixture:=authoredContinuationFixture(t);_,sha,registered,err:=CompileAuthoredFinalContinuation(fixture);if err!=nil{t.Fatal(err)}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 compiled,err:=MaterializeAuthoredFinalParams(fixture,ref);if err!=nil{t.Fatal(err)}
 if compiled.Params.GeometryContinuation!=ref||compiled.Params.RegisteredOriginal!=registered||compiled.Params.Recipe!=fixture.Inventory.Context.OriginalRecipe||compiled.Params.Source!=fixture.Inventory.Context.Source{t.Fatal("published/current/original identities changed")}
 ref.SHA256=fixture.Inventory.BaseReceipt.SHA256
 if _,err=MaterializeAuthoredFinalParams(fixture,ref);err==nil{t.Fatal("unrelated publication accepted")}
}
