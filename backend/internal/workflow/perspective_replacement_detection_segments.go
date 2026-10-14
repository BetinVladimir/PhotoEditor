package workflow

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)

// ReplacementDetectionSegmentsPlan owns deterministic publication content. It
// carries no reservation/activation authority; mutations call LockSegmentsPlan.
type ReplacementDetectionSegmentsPlan struct {
 descriptor string
 snapshot,result uuid.UUID
 detectorRoot v2input.Ref
 scope artifactjson.Scope
 artifact uuid.UUID
 body,sha string
}
func(p ReplacementDetectionSegmentsPlan) Scope()artifactjson.Scope{return p.scope}
func(p ReplacementDetectionSegmentsPlan) Ref()v2input.Ref{return v2input.Ref{ArtifactID:p.artifact,SHA256:p.sha}}
func(p ReplacementDetectionSegmentsPlan) Bytes()[]byte{return []byte(p.body)}

func(r ReplacementDetectionDocuments) compileSegmentsPlan(ctx context.Context,locked LockedReplacementDetectedLines)(ReplacementDetectionSegmentsPlan,error){
 var zero ReplacementDetectionSegmentsPlan
 if err:=ctx.Err();err!=nil{return zero,err}
 if locked.descriptor!=string(r.Plan.Descriptor())||locked.snapshot!=r.Snapshot||r.Snapshot==uuid.Nil{return zero,v2input.ErrExpired}
 body,sha,err:=locked.LinesDocument();if err!=nil{return zero,err}
 d,err:=decodeReplacementDetectionDescriptor([]byte(locked.descriptor));if err!=nil{return zero,err}
 if d.Base!=locked.observation.Detection.Base{return zero,v2input.ErrExpired}
 // Versioned namespace prevents collisions with detector Params/transport and
 // any future document format. Root/result binding prevents stale replay.
 seed:="V29.ReplacementSegments/v1:"+locked.observation.ResultID.String()+":"+locked.Root().ArtifactID.String()+":"+locked.Root().SHA256+":"+sha
 owner:=uuid.NewSHA1(d.Task,[]byte(seed))
 return ReplacementDetectionSegmentsPlan{descriptor:locked.descriptor,snapshot:locked.snapshot,result:locked.observation.ResultID,detectorRoot:locked.Root(),scope:artifactjson.Scope{Tenant:d.Tenant,Run:d.Run,Owner:owner,Purpose:"task_params"},artifact:uuid.NewSHA1(owner,[]byte("params")),body:string(body),sha:sha},ctx.Err()
}

func(r ReplacementDetectionDocuments) PrepareSegmentsPlan(ctx context.Context)(ReplacementDetectionSegmentsPlan,error){
 var plan ReplacementDetectionSegmentsPlan
 err:=r.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  locked,err:=r.LockDetectedLines(ctx,tx);if err!=nil{return err}
  plan,err=r.compileSegmentsPlan(ctx,locked);return err
 })
 if err!=nil{return ReplacementDetectionSegmentsPlan{},err};return plan,nil
}

// LockSegmentsPlan reacquires producer/source locks and reconstructs exact
// content before each reserve/complete/final admission. Caller owns commit.
func(r ReplacementDetectionDocuments) LockSegmentsPlan(ctx context.Context,tx pgx.Tx,expected ReplacementDetectionSegmentsPlan)(LockedReplacementDetectedLines,error){
 var zero LockedReplacementDetectedLines
 if expected.descriptor!=string(r.Plan.Descriptor())||expected.snapshot!=r.Snapshot||len(expected.body)<2||len(expected.body)>1<<20||expected.result==uuid.Nil{return zero,v2input.ErrInvalid}
 locked,err:=r.LockDetectedLines(ctx,tx);if err!=nil{return zero,err}
 actual,err:=r.compileSegmentsPlan(ctx,locked);if err!=nil{return zero,err}
 if expected.scope!=actual.scope||expected.artifact!=actual.artifact||expected.sha!=actual.sha||expected.result!=actual.result||expected.detectorRoot!=actual.detectorRoot||!bytes.Equal(expected.Bytes(),actual.Bytes()){return zero,v2input.ErrExpired}
 return locked,ctx.Err()
}
