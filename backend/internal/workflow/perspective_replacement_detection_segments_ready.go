package workflow

import (
 "context"
 "bytes"
 "io"
 "photoeditor/backend/internal/objectstore"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)

// ReadyReplacementSegments is metadata evidence bound to exact current lines.
// Object bytes and locator must still be verified outside a transaction before
// use; final publisher must repeat LockReadySegments through its own commit.
type ReadyReplacementSegments struct {
 plan ReplacementDetectionSegmentsPlan
 bucket,key,version string
}
func(s ReadyReplacementSegments) Ref()v2input.Ref{return s.plan.Ref()}
func(s ReadyReplacementSegments) Bytes()[]byte{return s.plan.Bytes()}
func(r ReplacementDetectionDocuments) LockReadySegments(ctx context.Context,tx pgx.Tx,plan ReplacementDetectionSegmentsPlan)(ReadyReplacementSegments,error){
 var zero ReadyReplacementSegments
 if err:=ctx.Err();err!=nil{return zero,err}
 if tx==nil{return zero,v2input.ErrInvalid}
 repo:=replacementSegmentsRepository{source:r,plan:plan}
 if err:=repo.validate(plan.Scope(),plan.sha,int64(len(plan.body)));err!=nil{return zero,err}
 proof,err:=repo.lockedProof(ctx,tx);if err!=nil{return zero,err}
 var id uuid.UUID;var key,version string
 s:=plan.Scope()
 err=tx.QueryRow(ctx,"SELECT artifact_id,object_key,object_version FROM photo.read_v29_replacement_segments_ready($1,$2,$3,$4,$5,$6,$7::jsonb)",s.Tenant,s.Run,s.Owner,plan.sha,int64(len(plan.body)),r.Bucket,string(proof)).Scan(&id,&key,&version)
 if errors.Is(err,pgx.ErrNoRows){return zero,v2input.ErrExpired};if err!=nil{return zero,err}
 expected,err:=artifactjson.DocumentKey(s,plan.artifact)
 if err!=nil||id!=plan.artifact||key!=expected||version==""||version=="null"||len(version)>1024{return zero,v2input.ErrInvalid}
 if err=ctx.Err();err!=nil{return zero,err}
 return ReadyReplacementSegments{plan:plan,bucket:r.Bucket,key:key,version:version},nil
}
func(r ReplacementDetectionDocuments) ReadReadySegments(ctx context.Context,plan ReplacementDetectionSegmentsPlan)(ReadyReplacementSegments,error){
 var out ReadyReplacementSegments
 err:=r.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  var err error;out,err=r.LockReadySegments(ctx,tx,plan);return err
 })
 if err!=nil{return ReadyReplacementSegments{},err};return out,nil
}

// VerifyReadySegments performs bounded versioned object IO outside SQL locks.
// This witness does not survive a source change; final admission repeats locks.
func(r ReplacementDetectionDocuments) VerifyReadySegments(ctx context.Context,plan ReplacementDetectionSegmentsPlan,objects v2input.SessionAnalysisObjectReader)(ReadyReplacementSegments,error){
 var zero ReadyReplacementSegments
 if err:=ctx.Err();err!=nil{return zero,err}
 if objects==nil{return zero,v2input.ErrInvalid}
 located,err:=r.ReadReadySegments(ctx,plan);if err!=nil{return zero,err}
 stream,err:=objects.Open(ctx,objectstore.Object{Key:located.key,VersionID:located.version,SHA256:plan.sha,Bytes:int64(len(plan.body)),ContentType:"application/json"})
 if err!=nil{if stream!=nil{_ = stream.Close()};return zero,err}
 if stream==nil{return zero,v2input.ErrInvalid}
 body,readErr:=io.ReadAll(io.LimitReader(stream,int64(len(plan.body))+1))
 closeErr:=stream.Close()
 if err=ctx.Err();err!=nil{return zero,err}
 if readErr!=nil{return zero,readErr};if closeErr!=nil{return zero,closeErr}
 if !bytes.Equal(body,plan.Bytes()){return zero,v2input.ErrInvalid}
 current,err:=r.ReadReadySegments(ctx,plan);if err!=nil{return zero,err}
 if current!=located{return zero,v2input.ErrExpired}
 return current,ctx.Err()
}
