package v2render

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// The unexported frozen fields retain physically verified qualification bytes
// and exact current scheduler inventory. Stage must still reacquire SQL locks;
// this preparation cannot authorize an arbitrary ready Params/face artifact.
type QualifiedAuthoredDefectProposalPlan struct {
 native AuthoredDefectProposalPlan
 source AuthoredInstructionSource
 qualification AuthoredDefectQualification
 expected,reportJCS,inventoryJCS string
 paramsRef v2input.Ref
 paramsJCS string
}
type SQLAuthoredDefectProposalPreparation struct {
 Scheduler *pgxpool.Pool
 Qualification SQLAuthoredDefectQualification
}
// Prepare reuses the real native CompileSemanticDefectTask. Its caller supplies
// an actually published, source-authorized Params body and complete native input
// metadata; the dedicated publication/current scope remains a separate guard.
func(s SQLAuthoredDefectProposalPreparation)Prepare(ctx context.Context,who auth.Identity,tenant,parent uuid.UUID,ordinal int,
 expected v2qualification.Expected,report v2input.Ref,imageBody,paramsBody []byte,paramsRef v2input.Ref,
 entries []v2input.Entry,sizes []v2input.SemanticInputSize,ram,scratch,output int64)(QualifiedAuthoredDefectProposalPlan,error){
 empty:=QualifiedAuthoredDefectProposalPlan{}
 if err:=ctx.Err();err!=nil{return empty,err}
 if s.Scheduler==nil{return empty,v2input.ErrInvalid}
 // Copy caller-owned inputs before IO. This does not make concurrent mutation
 // during the initial copy safe; configuration publication must be synchronized.
 expected=ownAuthoredDefectExpected(expected)
 imageBody=append([]byte(nil),imageBody...);paramsBody=append([]byte(nil),paramsBody...)
 entries=append([]v2input.Entry(nil),entries...);sizes=append([]v2input.SemanticInputSize(nil),sizes...)
 inventory,found,err:=ReadAuthoredSegmentInventory(ctx,s.Scheduler,tenant,parent,ordinal)
 if err!=nil{return empty,err};if !found{return empty,v2input.ErrExpired}
 source:=inventory.Context
 if source.Actor!=who.UserID||source.Session!=who.SessionID{return empty,v2input.ErrInvalid}
 proof,err:=s.Qualification.Read(ctx,who,tenant,source.Project,expected,report);if err!=nil{return empty,err}
 native,err:=CompileAuthoredDefectProposalTask(ctx,source,imageBody,paramsBody,paramsRef,entries,sizes,proof.costs,ram,0,scratch,output)
 if err!=nil{return empty,err}
 raw,err:=v2input.ReadPinnedSemanticDocument(ctx,s.Qualification.Objects,proof.object,proof.report);if err!=nil{return empty,err}
 verified,err:=v2qualification.Verify(raw,proof.report.SHA256,expected);if err!=nil{return empty,err}
 costs,err:=authoredDefectMeasuredCosts(verified.Metrics);if err!=nil{return empty,err};if costs!=proof.costs{return empty,v2input.ErrConflict}
 if err=s.Qualification.Recheck(ctx,who,tenant,source.Project,expected,proof);err!=nil{return empty,err}
 after,present,err:=ReadAuthoredSegmentInventory(ctx,s.Scheduler,tenant,parent,ordinal)
 if err!=nil{return empty,err};if !present{return empty,v2input.ErrExpired}
 beforeJCS,err:=canonical.Marshal(inventory);if err!=nil{return empty,err};afterJCS,err:=canonical.Marshal(after);if err!=nil{return empty,err}
 if !bytes.Equal(beforeJCS,afterJCS){return empty,v2input.ErrExpired}
 expectedJCS,err:=canonical.Marshal(expected);if err!=nil{return empty,err}
 if err=ctx.Err();err!=nil{return empty,err}
 return QualifiedAuthoredDefectProposalPlan{native:native,source:source,qualification:proof,expected:string(expectedJCS),reportJCS:string(raw),inventoryJCS:string(beforeJCS),paramsRef:paramsRef,paramsJCS:string(paramsBody)},nil
}
