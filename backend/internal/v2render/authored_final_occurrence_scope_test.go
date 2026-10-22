package v2render

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func TestAuthoredFinalSegmentOccurrencePrefixIsolation(t *testing.T) {
 tenant,parent,workflow,instruction:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
 inventory:=AuthoredFinalInventory{BaseReceipt:ref(),Phases:[]AuthoredPublishedPhase{{Receipt:ref()}}}
 inventory.Context.Workflow=workflow;inventory.Context.ProgramSHA="program";inventory.Context.OriginalRecipe=ref()
 source:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:"canvas"}
 phase:=AuthoredPhaseReceipt{Kind:"segment",Tenant:tenant,Parent:parent,Ordinal:2,Instruction:instruction,InputSource:source,PredecessorSHA:"prefix"}
 p:=AuthoredSegmentParams{TenantID:tenant,ParentRunID:parent,WorkflowID:workflow,ProgramSHA256:"program",Recipe:inventory.Context.OriginalRecipe,BaseReceipt:inventory.BaseReceipt,Ordinal:2,InstructionID:instruction,Source:source,PredecessorSHA256:"prefix",PhaseReceipts:[]v2input.Ref{inventory.Phases[0].Receipt}}
 if !authoredFinalSegmentOccurrenceMatches(p,phase,inventory,tenant,parent){t.Fatal("exact adopted historical occurrence refused")}
 cases:=[]struct{name string;mutate func(*AuthoredSegmentParams)}{
 {"foreign tenant",func(v *AuthoredSegmentParams){v.TenantID=uuid.New()}},
 {"foreign parent",func(v *AuthoredSegmentParams){v.ParentRunID=uuid.New()}},
 {"foreign workflow",func(v *AuthoredSegmentParams){v.WorkflowID=uuid.New()}},
 {"wrong source",func(v *AuthoredSegmentParams){v.Source.Source=ref()}},
 {"wrong instruction",func(v *AuthoredSegmentParams){v.InstructionID=uuid.New()}},
 {"missing prefix",func(v *AuthoredSegmentParams){v.PhaseReceipts=nil}},
 {"substituted prefix",func(v *AuthoredSegmentParams){v.PhaseReceipts=[]v2input.Ref{ref()}}},
 {"extra prefix",func(v *AuthoredSegmentParams){v.PhaseReceipts=append(v.PhaseReceipts,ref())}},
 {"stale predecessor",func(v *AuthoredSegmentParams){v.PredecessorSHA256="other"}},
 }
 for _,c:=range cases{t.Run(c.name,func(t *testing.T){v:=p;v.PhaseReceipts=append([]v2input.Ref(nil),p.PhaseReceipts...);c.mutate(&v);if authoredFinalSegmentOccurrenceMatches(v,phase,inventory,tenant,parent){t.Fatal("foreign occurrence accepted")}})}
 phase.Kind="geometry";if authoredFinalSegmentOccurrenceMatches(p,phase,inventory,tenant,parent){t.Fatal("geometry relabel accepted")}
}
