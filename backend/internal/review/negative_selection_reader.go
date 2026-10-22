package review

import (
 "bytes"
 "context"
 "errors"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

func(s *Service)ReadNegativeResultSelection(ctx context.Context,who auth.Identity,tid,reviewID uuid.UUID)(NegativeResultSelection,error){
 var out NegativeResultSelection
 if s==nil||s.DB==nil{return out,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 if tid==uuid.Nil||reviewID==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  var snapshot CandidateSnapshot;var projectID,taskID,actor uuid.UUID
  var decision,route,hash string;var selection []byte
  err:=tx.QueryRow(ctx,`SELECT w.project_id,v.task_id,v.run_id,v.decided_by,v.decision,v.decision_route,v.decision_hash,v.selection,
   v.candidate_artifact_id,v.candidate_hash,v.base_artifact_id,b.sha256
   FROM photo.reviews v JOIN photo.tasks t ON t.tenant_id=v.tenant_id AND t.id=v.task_id AND t.run_id=v.run_id
   JOIN photo.runs r ON r.tenant_id=v.tenant_id AND r.id=v.run_id
   JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
   JOIN photo.artifacts b ON b.tenant_id=v.tenant_id AND b.id=v.base_artifact_id
   WHERE v.tenant_id=$1 AND v.id=$2 AND v.kind='optical.negative' AND v.state='decided' AND v.revision=2
   AND v.decided_by IS NOT NULL AND v.decided_at IS NOT NULL FOR SHARE OF v,t,r`,tid,reviewID).Scan(&projectID,&taskID,&snapshot.RunID,&actor,&decision,&route,&hash,&selection,&snapshot.Candidate.ID,&snapshot.Candidate.SHA256,&snapshot.Base.ID,&snapshot.Base.SHA256)
  if errors.Is(err,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)}
  if err!=nil{return err}
  // Row locks may wait after the initial tenant role was read. Refresh it
  // before using the admin bypass in project authorization.
  role,err=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor)
  if err!=nil{return err}
  if _,err:=project.Authorize(ctx,tx,tid,projectID,who.UserID,role,domain.Editor,false);err!=nil{return err}
  snapshot.TenantID=tid;snapshot.ReviewID=reviewID;snapshot.Kind="optical.negative";snapshot.State="pending";snapshot.Revision=1
  request:=Decision{ExpectedRevision:1,CandidateHash:snapshot.Candidate.SHA256,Decision:decision}
  plan,err:=PlanCandidate(snapshot,actor,request);if err!=nil{return err}
  expected,err:=canonical.Marshal(plan.Selection)
  normalized,normalizeErr:=canonical.Bytes(selection)
  if err!=nil||normalizeErr!=nil||!bytes.Equal(expected,normalized)||plan.DecisionHash!=hash||plan.Route!=route{return domain.NewError("STALE_CANDIDATE_REVIEW",409)}
  compiled,err:=CompileNegativeResultSelection(snapshot,actor,request,plan);if err!=nil{return err}
  var selectedReview uuid.UUID;var selectedHash,selectedRoute string;var imageID *uuid.UUID;var imageSHA *string
  err=tx.QueryRow(ctx,negativeSelectionAuthoritySQL,tid,taskID).Scan(&selectedReview,&selectedHash,&selectedRoute,&imageID,&imageSHA)
  if errors.Is(err,pgx.ErrNoRows){return domain.NewError("STALE_CANDIDATE_REVIEW",409)}
  if err!=nil{return err}
  if selectedReview!=reviewID||selectedHash!=hash||selectedRoute!=route||(imageID==nil)!=(compiled.Image==nil)||(imageSHA==nil)!=(compiled.Image==nil){return domain.NewError("STALE_CANDIDATE_REVIEW",409)}
  if compiled.Image!=nil&&(*imageID!=compiled.Image.ID||*imageSHA!=compiled.Image.SHA256){return domain.NewError("STALE_CANDIDATE_REVIEW",409)}
  out=compiled;return ctx.Err()
 })
 if err!=nil{return NegativeResultSelection{},err}
 return out,nil
}

const negativeSelectionAuthoritySQL = ` SELECT v.id,v.decision_hash,v.decision_route,
 CASE v.decision_route WHEN 'candidate' THEN c.id WHEN 'base' THEN b.id ELSE NULL END,
 CASE v.decision_route WHEN 'candidate' THEN c.sha256 WHEN 'base' THEN b.sha256 ELSE NULL END
 FROM photo.reviews v JOIN photo.tasks t ON t.tenant_id=v.tenant_id AND t.id=v.task_id AND t.run_id=v.run_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 JOIN photo.artifacts c ON c.tenant_id=v.tenant_id AND c.id=v.candidate_artifact_id AND c.sha256=v.candidate_hash
 JOIN photo.artifacts b ON b.tenant_id=v.tenant_id AND b.id=v.base_artifact_id
 JOIN photo.v2_artifact_links root_link ON root_link.tenant_id=w.tenant_id AND root_link.workflow_id=w.id
 AND root_link.project_id=w.project_id AND root_link.ordinal=0
 AND root_link.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex')
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id AND root.id=root_link.artifact_id AND root.sha256=root_link.sha256
 WHERE v.tenant_id=$1 AND v.task_id=$2 AND v.kind='optical.negative'
 AND t.operation=v.kind AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
 AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
 AND v.state='decided' AND v.revision=2 AND v.decided_by IS NOT NULL AND v.decided_at IS NOT NULL
 AND v.decision_hash ~ '^[0-9a-f]{64}$'
 AND ((v.decision='accept' AND v.decision_route='candidate') OR (v.decision='reject' AND v.decision_route='base')
 OR (v.decision='skip' AND v.decision_route='skip'))
 AND v.selection=jsonb_build_object('version','candidate_review_v1','candidate',jsonb_build_object('artifact_id',c.id::text,'sha256',c.sha256),
 'base',jsonb_build_object('artifact_id',b.id::text,'sha256',b.sha256),'revision',2)
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token AND z.review_required
 AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.id::text,'sha256',root.sha256))
 AND root.created_by_task_id=t.id AND root.produced_attempt=t.attempt AND root.produced_fence=t.fencing_token
 AND c.created_by_task_id=t.id AND c.produced_attempt=t.attempt AND c.produced_fence=t.fencing_token
 AND root.state='ready' AND c.state='ready' AND b.state='ready'
 AND root.verified_at IS NOT NULL AND c.verified_at IS NOT NULL AND b.verified_at IS NOT NULL
 AND root.object_version NOT IN('','null') AND c.object_version NOT IN('','null') AND b.object_version NOT IN('','null')
 AND root.media_type='application/json' AND c.media_type='application/json' AND b.media_type='application/json'
 AND root.output_role='document' AND c.output_role='working_image'
 AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 AND (b.expires_at IS NULL OR b.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.project_id=w.project_id
 AND i.artifact_id=b.id AND i.sha256=b.sha256 AND i.contract='WorkingImage')
 AND EXISTS(SELECT 1 FROM photo.v2_artifact_links child WHERE child.tenant_id=w.tenant_id AND child.workflow_id=w.id
 AND child.project_id=w.project_id AND child.artifact_id=c.id AND child.sha256=c.sha256
 AND child.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex'))
 AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
 AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 AND NOT EXISTS(SELECT 1 FROM photo.reviews other WHERE other.tenant_id=t.tenant_id AND other.task_id=t.id AND other.id<>v.id)`
