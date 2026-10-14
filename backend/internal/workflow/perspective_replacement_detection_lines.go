package workflow

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

// ReplacementDetectedLines is an observation, not publication authority.
type ReplacementDetectedLines struct {
 ResultID uuid.UUID
 Root v2input.Ref
 Detection v2optical.DetectedPerspectiveLines
}

// LockedReplacementDetectedLines owns exact adopted bytes. It is valid only
// while the caller transaction remains open; a publisher must acquire it anew
// in every mutation transaction. Private fields prevent caller construction.
type LockedReplacementDetectedLines struct {
 observation ReplacementDetectedLines
 resultJCS string
 descriptor string
 snapshot uuid.UUID
}
func(l LockedReplacementDetectedLines) ResultBytes()[]byte{return []byte(l.resultJCS)}
func(l LockedReplacementDetectedLines) Root()v2input.Ref{return l.observation.Root}
func(l LockedReplacementDetectedLines) Status()string{return l.observation.Detection.Status}

// LockDetectedLines retains source, producer, result and object metadata locks
// through caller commit. It performs no IO or transaction commit of its own.
func(r ReplacementDetectionDocuments) LockDetectedLines(ctx context.Context,tx pgx.Tx)(LockedReplacementDetectedLines,error){
 var zero LockedReplacementDetectedLines
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return zero,err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return zero,err}
 if tx==nil{return zero,v2input.ErrInvalid}
 if err=replacementDetectionAdoptionTxScope(ctx,tx,scope.Tenant);err!=nil{return zero,err}
 step,proof,err:=r.Prepared.PerspectiveDetectionCurrentProof(ctx,tx,r.Rebuilt,r.Retained,r.Mappings,r.Unchanged,r.Plan.Descriptor(),r.Snapshot);if err!=nil{return zero,err}
 if _,err=RestorePerspectiveReplacementDetectionPlan(ctx,step,r.Plan.Descriptor());err!=nil{return zero,err}
 var out ReplacementDetectedLines;var raw string
 err=tx.QueryRow(ctx,"SELECT result_id,artifact_id,sha256,result_jcs FROM photo.read_v29_replacement_detector_lines($1,$2,$3::jsonb)",scope.Tenant,scope.Owner,string(proof)).Scan(&out.ResultID,&out.Root.ArtifactID,&out.Root.SHA256,&raw)
 if errors.Is(err,pgx.ErrNoRows){return zero,v2input.ErrExpired};if err!=nil{return zero,err}
 if out.ResultID==uuid.Nil{return zero,v2input.ErrInvalid}
 out.Detection,err=v2optical.ParseDetectedPerspectiveLines([]byte(raw),out.Root,r.Plan.Detector().Scope().Base.AssetVersionID.String())
 if err!=nil{return zero,err}
 if out.Detection.Base!=r.Plan.Detector().Scope().Base{return zero,v2input.ErrExpired}
 if err=ctx.Err();err!=nil{return zero,err}
 return LockedReplacementDetectedLines{observation:out,resultJCS:raw,descriptor:string(r.Plan.Descriptor()),snapshot:r.Snapshot},nil
}
func(r ReplacementDetectionDocuments) ReadDetectedLines(ctx context.Context)(ReplacementDetectedLines,error){
 var out ReplacementDetectedLines
 err:=r.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  locked,err:=r.LockDetectedLines(ctx,tx);if err!=nil{return err}
  out=locked.observation;return nil
 })
 if err!=nil{return ReplacementDetectedLines{},err};return out,nil
}

// LinesDocument compiles the distinct segment-document contract expected by
// optical.perspective. The detector root is never reused as a segment artifact.
// Returned bytes still require dedicated immutable publication and fresh locks.
func(l LockedReplacementDetectedLines) LinesDocument()([]byte,string,error){
 if l.snapshot==uuid.Nil||len(l.descriptor)<2||l.observation.ResultID==uuid.Nil||l.observation.Detection.Status!="available"||l.observation.Detection.Lines==nil{return nil,"",v2input.ErrInvalid}
 detection,err:=v2optical.ParseDetectedPerspectiveLines([]byte(l.resultJCS),l.observation.Root,l.observation.Detection.Base.AssetVersionID.String())
 if err!=nil||detection.Base!=l.observation.Detection.Base||detection.Status!="available"||detection.Lines==nil{return nil,"",v2input.ErrInvalid}
 return detection.Lines.Compile()
}
