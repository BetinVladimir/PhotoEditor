package v2region

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2render"
)

// AuthoredRegionRender uses the normal renderer admission, input publication,
// recovery and activation path. It never writes a task result or adoption receipt.
// Freeze is owned metadata only: its SQL authority repeats exact current draft,
// source/version, actor/session and compound child identity under ordered locks.
type AuthoredRegionRender struct {
 Scheduler *pgxpool.Pool
 Submission v2render.SQLRenderSubmissionService
}

func (s AuthoredRegionRender) Start(ctx context.Context, record AuthoredRegionRecord) error {
 if err:=record.validate();err!=nil{return err}
 intent := record.Intent
 hash, err := intent.SHA256()
 if err != nil || record.ID == uuid.Nil || hash != record.SHA || record.CancelRequested || !record.Current {
  return v2input.ErrExpired
 }
 key, err := intent.RequestChildKey(record.ID,"render")
 if err != nil { return err }
 expected, err := v2input.RunID(intent.Tenant,intent.Actor,key)
 if err != nil || expected != record.RenderBatch { return ErrRegionInvalid }
 if err = requireAuthoredPoolRole(ctx,s.Scheduler,"photo_scheduler"); err != nil { return err }
 var snapshot uuid.UUID
 if err=s.Scheduler.QueryRow(ctx,"SELECT photo.freeze_authored_region_render_snapshot($1,$2,$3)",intent.Tenant,record.ID,record.SHA).Scan(&snapshot);err!=nil{return err}
 if snapshot==uuid.Nil{return v2input.ErrExpired}
 // A private copy prevents a request from changing another caller's factory.
 submission:=s.Submission
 if submission.Factory.Scheduler!=s.Scheduler{return ErrRegionInvalid}
 pin:=intent.Qualification
 submission.Factory.QualificationPin=&pin
 ticket,err:=submission.Queue(ctx,auth.Identity{UserID:intent.Actor,SessionID:intent.Session},intent.Tenant,intent.Project,key,
  v2render.RenderBatchSubmissionRequest{SnapshotID:snapshot,PolicySHA:intent.RenderPolicySHA,
   Selection:[]v2render.RenderSelectionItem{{Binding:intent.Binding,Recipe:intent.Recipe}}})
 if err!=nil{return err}
 if ticket.RunID!=record.RenderBatch||len(ticket.ChildRunIDs)!=1||ticket.ChildRunIDs[0]!=uuid.NewSHA1(record.RenderBatch,[]byte("render.batch:0")){return ErrRegionInvalid}
 // The returned ticket acknowledges admission, never canonical completion.
 return nil
}

func(s AuthoredRegionRender)Cancel(ctx context.Context,record AuthoredRegionRecord)error{
 if err:=requireAuthoredPoolRole(ctx,s.Scheduler,"photo_scheduler");err!=nil{return err}
 return (v2render.SQLRenderDraftStore{DB:s.Scheduler}).CancelBatch(ctx,record.Intent.Tenant,record.RenderBatch,
  auth.Identity{UserID:record.Intent.Actor,SessionID:record.Intent.Session})
}
