package v2optical

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type negativeSelectedSource struct {
 SchemaVersion int `json:"schema_version"`
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Review uuid.UUID `json:"review_id"`
 Task uuid.UUID `json:"origin_task_id"`
 Run uuid.UUID `json:"origin_run_id"`
 Hash string `json:"decision_hash"`
 Route string `json:"route"`
 Source *negativeSourceLocator `json:"source"`
}
func decodeNegativeSelectedSource(raw []byte,tenant,project uuid.UUID,d NegativeDeclinedDecision,bucket string)(negativeSelectedSource,error){
 var zero negativeSelectedSource
 if len(raw)<2||len(raw)>32768 {return zero,v2input.ErrExpired}
 normal,err:=canonical.Bytes(raw);if err!=nil {return zero,v2input.ErrInvalid}
 var out negativeSelectedSource
 if json.Unmarshal(normal,&out)!=nil {return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(out)
 if err!=nil||!bytes.Equal(exact,normal)||out.SchemaVersion!=1||out.Tenant!=tenant||out.Project!=project||out.Review!=d.Review||out.Task!=d.OriginTask||out.Run!=d.OriginRun||out.Hash!=d.DecisionHash||out.Route!=d.Route {return zero,v2input.ErrInvalid}
 if d.Route=="skip" {if out.Source!=nil {return zero,v2input.ErrInvalid};return out,nil}
 if d.Route!="base"||out.Source==nil {return zero,v2input.ErrInvalid}
 s:=out.Source
 if !validPerspectiveRef(v2input.Ref{ArtifactID:s.ArtifactID,SHA256:s.SHA256})||s.Bucket!=bucket||s.Key==""||len(s.Key)>1024||s.Version==""||s.Version=="null"||len(s.Version)>1024||s.Media!="application/json"||s.Bytes<2||s.Bytes>65536 {return zero,v2input.ErrInvalid}
 return out,nil
}

type negativeSelectionCatalog interface {lookup(context.Context,auth.Identity,uuid.UUID,uuid.UUID,NegativeDeclinedDecision)(negativeSelectedSource,error)}
type SQLNegativeSelectionCatalog struct {DB NegativeTransactions;Bucket string}
func(s SQLNegativeSelectionCatalog)lookup(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,d NegativeDeclinedDecision)(negativeSelectedSource,error){
 var zero negativeSelectedSource
 if err:=ctx.Err();err!=nil {return zero,err}
 if s.DB==nil||s.Bucket==""||tenant==uuid.Nil||project==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil {return zero,v2input.ErrInvalid}
 body,err:=canonical.Marshal([]NegativeDeclinedDecision{d});if err!=nil {return zero,err}
 if _,err=decodeNegativeDeclinedDecisions(body,uuid.Nil,1);err!=nil {return zero,err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=s.DB.Begin(ctx);if err!=nil {return zero,err};if tx==nil {return zero,v2input.ErrInvalid};defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil {return zero,err};if role!="photo_scheduler" {return zero,v2input.ErrInvalid}
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil {return zero,err}
 var raw []byte
 if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_selected_source($1,$2,$3,$4,$5,$6,$7,$8,$9)",tenant,project,who.UserID,who.SessionID,d.Review,d.OriginTask,d.OriginRun,d.DecisionHash,d.Route).Scan(&raw);err!=nil {return zero,err}
 out,err:=decodeNegativeSelectedSource(raw,tenant,project,d,s.Bucket);if err!=nil {return zero,err}
 if err:=tx.Commit(ctx);err!=nil {return zero,err};return out,nil
}

// VerifiedNegativeSelection owns bytes read under an exact immutable decision.
// It proves the WorkingImage document only, not its physical array closure or
// current replacement admission. It is not constructible from a caller's ref.
type VerifiedNegativeSelection struct {source negativeSelectedSource;raw string}
func(v VerifiedNegativeSelection)SelectedBase() *v2input.Ref {
 if v.source.Source==nil {return nil};return &v2input.Ref{ArtifactID:v.source.Source.ArtifactID,SHA256:v.source.Source.SHA256}
}
func(v VerifiedNegativeSelection)WorkingImageBytes()[]byte{return []byte(v.raw)}
func LoadNegativeSelectedSource(ctx context.Context,db NegativeTransactions,objects v2input.SessionAnalysisObjectReader,bucket string,who auth.Identity,tenant,project uuid.UUID,d NegativeDeclinedDecision)(VerifiedNegativeSelection,error){
 return loadNegativeSelectedSource(ctx,SQLNegativeSelectionCatalog{DB:db,Bucket:bucket},objects,who,tenant,project,d)
}
func loadNegativeSelectedSource(ctx context.Context,catalog negativeSelectionCatalog,objects v2input.SessionAnalysisObjectReader,who auth.Identity,tenant,project uuid.UUID,d NegativeDeclinedDecision)(VerifiedNegativeSelection,error){
 var zero VerifiedNegativeSelection
 if catalog==nil||objects==nil {return zero,v2input.ErrInvalid}
 before,err:=catalog.lookup(ctx,who,tenant,project,d);if err!=nil {return zero,err}
 if before.Source!=nil {owned:=*before.Source;before.Source=&owned}
 var raw []byte
 if before.Source!=nil {
  locator:=*before.Source
  reader,err:=objects.Open(ctx,objectstore.Object{Key:locator.Key,VersionID:locator.Version,SHA256:locator.SHA256,Bytes:locator.Bytes,ContentType:locator.Media})
  if err!=nil {if reader!=nil {_=reader.Close()};return zero,err};if reader==nil {return zero,v2input.ErrInvalid}
  raw,err=io.ReadAll(io.LimitReader(reader,locator.Bytes+1));closeErr:=reader.Close()
  if err!=nil {return zero,err};if closeErr!=nil {return zero,closeErr};if err=ctx.Err();err!=nil {return zero,err}
  sum:=sha256.Sum256(raw)
  if int64(len(raw))!=locator.Bytes||hex.EncodeToString(sum[:])!=locator.SHA256 {return zero,v2input.ErrInvalid}
  image,err:=review.ParseWorkingImage(raw);if err!=nil||image.DType!="float32" {return zero,v2input.ErrInvalid}
  exact,err:=canonical.Marshal(image);if err!=nil||!bytes.Equal(exact,raw) {return zero,v2input.ErrInvalid}
 }
 after,err:=catalog.lookup(ctx,who,tenant,project,d);if err!=nil {return zero,err}
 a,err:=canonical.Marshal(before);if err!=nil {return zero,err};b,err:=canonical.Marshal(after)
 if err!=nil||!bytes.Equal(a,b) {return zero,v2input.ErrExpired}
 // Own the locator; a catalog implementation must not retain mutable evidence.
 if before.Source!=nil {owned:=*before.Source;before.Source=&owned}
 return VerifiedNegativeSelection{source:before,raw:string(raw)},nil
}

// CompileTopology binds the pure replacement topology to the exact physically
// loaded editorial selection. It still does not establish array/admission proof.
func(v VerifiedNegativeSelection)CompileTopology(ctx context.Context,tenant,project,reviewID uuid.UUID,decisionHash string,plan NegativeInvalidationPlan,inventory []NegativeDependencyTask,maxTasks,maxEdges int)(NegativeReplacementTopology,error){
 s:=v.source
 if s.SchemaVersion!=1||s.Tenant!=tenant||s.Project!=project||s.Review!=reviewID||s.Hash!=decisionHash||s.Task!=plan.OriginTask||s.Run!=plan.OriginRun||s.Route!=plan.Route {
  return NegativeReplacementTopology{},v2input.ErrInvalid
 }
 if (s.Route=="base"&&(s.Source==nil||v.raw==""))||(s.Route=="skip"&&(s.Source!=nil||v.raw!="")) {
  return NegativeReplacementTopology{},v2input.ErrInvalid
 }
 return CompileNegativeReplacementTopology(ctx,tenant,project,reviewID,decisionHash,plan,inventory,v.SelectedBase(),maxTasks,maxEdges)
}
