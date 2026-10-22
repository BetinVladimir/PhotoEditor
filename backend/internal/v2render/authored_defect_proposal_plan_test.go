package v2render

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func TestAuthoredDefectProposalCancellationBeforeSourceIO(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,err:=CompileAuthoredDefectProposalTask(ctx,AuthoredInstructionSource{},nil,nil,v2input.Ref{},nil,nil,v2input.SemanticQualifiedCosts{},0,0,0,0)
 if !errors.Is(err,context.Canceled){t.Fatalf("cancellation identity lost: %v",err)}
}

func TestAuthoredDefectProposalRejectsUnboundOccurrence(t *testing.T){
 parent:=uuid.New();workflow:=uuid.NewSHA1(parent,[]byte("workflow"));instruction,_:=v2input.TaskID(workflow,"render.authored_segment",1)
 source:=AuthoredInstructionSource{Tenant:uuid.New(),Parent:parent,Workflow:workflow,Instruction:AuthoredGeometryInstruction{Identity:instruction,Kind:"segment",Ordinal:1,Layers:[]uuid.UUID{uuid.New()}}}
 // No actual WorkingImage/current source is supplied. It must not be possible
 // to reach native proposal compilation from an identity marker alone.
 for _,mutate:=range []func(*AuthoredInstructionSource){
  func(s *AuthoredInstructionSource){},
  func(s *AuthoredInstructionSource){s.Instruction.Identity=uuid.New()},
  func(s *AuthoredInstructionSource){s.Instruction.Layers=nil},
  func(s *AuthoredInstructionSource){s.Instruction.Kind="geometry"},
  func(s *AuthoredInstructionSource){s.Workflow=uuid.New()},
 }{
  candidate:=source;mutate(&candidate)
  _,err:=CompileAuthoredDefectProposalTask(context.Background(),candidate,nil,nil,v2input.Ref{},nil,nil,v2input.SemanticQualifiedCosts{},1,0,1,1)
  if !errors.Is(err,v2input.ErrInvalid){t.Fatalf("unbound occurrence admitted: %v",err)}
 }
}
