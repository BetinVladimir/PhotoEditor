package v2render

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func authoredSegmentInventoryFixture(t *testing.T,enabled bool,opacity float64)AuthoredSegmentInventory{
 t.Helper();p:=authoredSegmentParamsFixture();layer:=uuid.New()
 raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"source":p.Source,"layers":[]any{map[string]any{"id":layer,"enabled":enabled,"opacity":opacity,"operation":"optical.perspective","operation_version":"1","params":p.Recipe}}});if err!=nil{t.Fatal(err)}
 return AuthoredSegmentInventory{Context:AuthoredInstructionSource{Tenant:p.TenantID,Parent:p.ParentRunID,Workflow:p.WorkflowID,ProgramSHA:p.ProgramSHA256,PredecessorSHA:p.PredecessorSHA256,Source:p.Source,OriginalRecipe:p.Recipe,LayerStackJCS:string(raw),Instruction:AuthoredGeometryInstruction{Kind:"segment",Ordinal:1,Identity:p.InstructionID,Layers:[]uuid.UUID{layer}}},BaseReceipt:p.BaseReceipt}
}

func TestMaterializeAuthoredSegmentPreservesDisabledGeometryOccurrence(t *testing.T){
 inventory:=authoredSegmentInventoryFixture(t,false,1)
 compiled,err:=MaterializeAuthoredSegmentParams(inventory,nil,nil);if err!=nil{t.Fatal(err)}
 if compiled.Params.CurrentLayers!=nil||compiled.Params.InstructionID!=inventory.Context.Instruction.Identity||compiled.Params.Source!=inventory.Context.Source||compiled.Params.PredecessorSHA256!=inventory.Context.PredecessorSHA{t.Fatal("noop occurrence/source authority changed")}
 active:=authoredSegmentParamsFixture().CurrentLayers
 if _,err=MaterializeAuthoredSegmentParams(inventory,active,nil);err==nil{t.Fatal("disabled occurrence relabelled active")}
}

func TestMaterializeAuthoredSegmentActiveAndPrefixMustBeExact(t *testing.T){
 inventory:=authoredSegmentInventoryFixture(t,true,.4);layers:=authoredSegmentParamsFixture().CurrentLayers
 compiled,err:=MaterializeAuthoredSegmentParams(inventory,layers,nil);if err!=nil{t.Fatal(err)}
 if *compiled.Params.CurrentLayers!=*layers||compiled.Params.Source!=inventory.Context.Source{t.Fatal("published layer/current source changed")}
 if _,err=MaterializeAuthoredSegmentParams(inventory,nil,nil);err==nil{t.Fatal("active occurrence silently dropped")}
 inventory.Context.Instruction.Ordinal=2
 if _,err=MaterializeAuthoredSegmentParams(inventory,layers,nil);err==nil{t.Fatal("missing canonical predecessor accepted")}
}
