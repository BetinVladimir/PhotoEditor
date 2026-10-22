package v2draft

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2portrait"
)

type GlareSourcePreparationRequest struct {
 SchemaVersion int `json:"schema_version"`
 Shoot uuid.UUID `json:"shoot_session_id"`
 Snapshot uuid.UUID `json:"snapshot_id"`
 Ordinal int `json:"ordinal"`
 Base v2input.SessionVersionBinding `json:"base"`
 Strength float64 `json:"strength"`
 GlassesMask v2input.Ref `json:"glasses_mask"`
 Protected v2input.Ref `json:"protected"`
 TargetPixel v2input.Ref `json:"target_pixel"`
 ProtectedPixel v2input.Ref `json:"protected_pixel"`
 SourceFaces v2input.Ref `json:"source_faces"`
 SourceObservation uuid.UUID `json:"source_observation_id"`
}
type GlareSourceClientIntent struct {
 SchemaVersion int `json:"schema_version"`
 Shoot uuid.UUID `json:"shoot_session_id"`
 Snapshot uuid.UUID `json:"snapshot_id"`
 Ordinal int `json:"ordinal"`
 Proposal json.RawMessage `json:"params_proposal"`
 SourceObservation uuid.UUID `json:"source_observation_id"`
}
func DecodeGlareSourcePreparationRequest(raw []byte)(GlareSourcePreparationRequest,error){
 var p GlareSourcePreparationRequest
 if len(raw)<2||len(raw)>16384||json.Unmarshal(raw,&p)!=nil{return p,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(p)
 if err!=nil||!bytes.Equal(exact,raw)||p.SchemaVersion!=1||p.Shoot==uuid.Nil||p.Snapshot==uuid.Nil||p.SourceObservation==uuid.Nil||p.Ordinal<0||p.Ordinal>=5000{return GlareSourcePreparationRequest{},v2input.ErrInvalid}
 // The issued owner/calibration are server inputs, not client authority.
 shape:=v2portrait.GlareSourceParams{SchemaVersion:1,Base:p.Base,Strength:p.Strength,Mode:"generative",GlassesMask:p.GlassesMask,Protected:p.Protected,SourceFaces:p.SourceFaces,Confirmation:v2input.Ref{ArtifactID:uuid.NameSpaceOID,SHA256:p.Base.Source.SHA256},Calibration:v2input.Ref{ArtifactID:uuid.NameSpaceDNS,SHA256:p.Base.Source.SHA256},ModelLockSHA:p.Base.Source.SHA256}
 body,err:=canonical.Marshal(shape);if err!=nil{return GlareSourcePreparationRequest{},err}
 if _,err=v2portrait.ParseGlareSourceParams(body);err!=nil{return GlareSourcePreparationRequest{},err}
 seen:=map[uuid.UUID]bool{}
 for _,r:=range []v2input.Ref{p.Base.Source,p.GlassesMask,p.Protected,p.SourceFaces,p.TargetPixel,p.ProtectedPixel}{if r.ArtifactID==uuid.Nil||seen[r.ArtifactID]{return GlareSourcePreparationRequest{},v2input.ErrInvalid};seen[r.ArtifactID]=true;if _,err=domain.ParseDigest(r.SHA256);err!=nil{return GlareSourcePreparationRequest{},v2input.ErrInvalid}}
 return p,nil
}
func DecodeGlareSourceClientIntent(raw []byte)(GlareSourceClientIntent,error){
 var p GlareSourceClientIntent
 if len(raw)<2||len(raw)>MaxGlareDonorIntentBytes||json.Unmarshal(raw,&p)!=nil{return p,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(p)
 if err!=nil||!bytes.Equal(exact,raw)||p.SchemaVersion!=1||p.Shoot==uuid.Nil||p.Snapshot==uuid.Nil||p.SourceObservation==uuid.Nil||p.Ordinal<0||p.Ordinal>=5000{return GlareSourceClientIntent{},v2input.ErrInvalid}
 if _,err=v2portrait.ParseGlareSourceParams(p.Proposal);err!=nil{return GlareSourceClientIntent{},err};return p,nil
}
func(s *GlareDonorSubmissionService)sourceTx(ctx context.Context,who auth.Identity,tenant,project uuid.UUID)(pgx.Tx,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s==nil||s.scheduler==nil||tenant!=s.tenant||project!=s.project||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return nil,v2input.ErrInvalid}
 tx,err:=s.scheduler.Begin(ctx);if err!=nil{return nil,err}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil{tx.Rollback(context.WithoutCancel(ctx));return nil,err}
 var role string;err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role)
 if err!=nil||role!="photo_scheduler"{tx.Rollback(context.WithoutCancel(ctx));if err==nil{err=v2input.ErrInvalid};return nil,err};return tx,nil
}
func(s *GlareDonorSubmissionService)PrepareSourceProjectKeyed(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,key string,r GlareSourcePreparationRequest)(GlareSourceClientIntent,error){
 var zero GlareSourceClientIntent
 raw,err:=canonical.Marshal(r);if err!=nil{return zero,err};r,err=DecodeGlareSourcePreparationRequest(raw);if err!=nil{return zero,err}
 if s==nil||!s.generativeEnabled{return zero,v2input.ErrInvalid}
 run,err:=v2input.RunID(tenant,who.UserID,key);if err!=nil{return zero,err};workflow,err:=v2input.WorkflowID(run);if err!=nil{return zero,err};owner,err:=v2input.TaskID(workflow,"portrait.glare",r.Ordinal);if err!=nil{return zero,err}
 p:=v2portrait.GlareSourceParams{SchemaVersion:1,Base:r.Base,Strength:r.Strength,Mode:"generative",GlassesMask:r.GlassesMask,Protected:r.Protected,SourceFaces:r.SourceFaces,Confirmation:v2input.Ref{ArtifactID:owner,SHA256:s.pin.ModelLockSHA},ModelLockSHA:s.pin.ModelLockSHA,Calibration:s.pin.Calibration}
 proposal,err:=canonical.Marshal(p);if err!=nil{return zero,err}
 tx,err:=s.sourceTx(ctx,who,tenant,project);if err!=nil{return zero,err};defer tx.Rollback(context.WithoutCancel(ctx))
 pin,err:=s.pin.Compile();if err!=nil{return zero,err};var qualified bool
 if err=tx.QueryRow(ctx,"SELECT photo.lock_v28_restoration_qualification($1,$2,$3,$4,'portrait.glare',$5::jsonb)",tenant,project,who.UserID,who.SessionID,string(pin)).Scan(&qualified);err!=nil{return zero,err};if !qualified{return zero,v2input.ErrExpired}
 _,sha,err:=v2portrait.BuildGlareSourceConfirmation(ctx,tx,who,tenant,project,r.Shoot,proposal,r.SourceObservation);if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err};if err=tx.Commit(ctx);err!=nil{return zero,err}
 if _,err=v2portrait.ReadManualPairOrigin(ctx,s.scheduler,who,tenant,project,r.Snapshot,r.Base,r.GlassesMask,r.Protected,r.TargetPixel,r.ProtectedPixel);err!=nil{return zero,err}
 p.Confirmation.SHA256=sha;proposal,err=canonical.Marshal(p);if err!=nil{return zero,err};if err=ctx.Err();err!=nil{return zero,err}
 return GlareSourceClientIntent{1,r.Shoot,r.Snapshot,r.Ordinal,proposal,r.SourceObservation},nil
}
func(s *GlareDonorSubmissionService)sourceTicket(ctx context.Context,who auth.Identity,tenant,project,run,workflow uuid.UUID,r GlareSourceClientIntent,profile []byte)(RestorationAdmissionTicket,error){
 var zero RestorationAdmissionTicket
 tx,err:=s.sourceTx(ctx,who,tenant,project);if err!=nil{return zero,err};defer tx.Rollback(context.WithoutCancel(ctx))
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v28_glare_source_admission_ticket($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULL)",tenant,run,who.UserID,who.SessionID,project,r.Shoot,r.Snapshot,string(r.Proposal),string(profile),r.SourceObservation).Scan(&raw);err!=nil{return zero,err}
 if len(raw)<2||len(raw)>4096{return zero,v2input.ErrExpired}
 var ticket RestorationAdmissionTicket;if json.Unmarshal(raw,&ticket)!=nil{return zero,v2input.ErrInvalid}
 normal,e:=canonical.Bytes(raw);exact,e2:=canonical.Marshal(ticket);expected,e3:=v2input.TaskID(workflow,"portrait.glare",r.Ordinal)
 if e!=nil||e2!=nil||e3!=nil||!bytes.Equal(normal,exact)||ticket.RunID!=run||ticket.WorkflowID!=workflow||ticket.TaskID!=expected||ticket.State!="queued"{return zero,v2input.ErrInvalid}
 if err=ctx.Err();err!=nil{return zero,err};if err=tx.Commit(ctx);err!=nil{return zero,err};return ticket,nil
}
func(s *GlareDonorSubmissionService)SubmitSourceProjectKeyed(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,key string,r GlareSourceClientIntent)(RestorationAdmissionTicket,error){
 var zero RestorationAdmissionTicket
 raw,err:=canonical.Marshal(r);if err!=nil{return zero,err};r,err=DecodeGlareSourceClientIntent(raw);if err!=nil{return zero,err}
 if s==nil||!s.generativeEnabled||tenant!=s.tenant||project!=s.project{return zero,v2input.ErrInvalid}
 p,err:=v2portrait.ParseGlareSourceParams(r.Proposal);if err!=nil{return zero,err}
 run,err:=v2input.RunID(tenant,who.UserID,key);if err!=nil{return zero,err};workflow,err:=v2input.WorkflowID(run);if err!=nil{return zero,err};owner,err:=v2input.TaskID(workflow,"portrait.glare",r.Ordinal);if err!=nil||owner!=p.Confirmation.ArtifactID{return zero,v2input.ErrInvalid}
 profile,err:=v2portrait.CompilePinnedRestorationProfile("portrait.glare",r.Proposal,s.profile,s.pin);if err!=nil{return zero,err}
 if ticket,e:=s.sourceTicket(ctx,who,tenant,project,run,workflow,r,profile);e==nil{return ticket,nil}else if !errors.Is(e,v2input.ErrExpired){return zero,e}
 proposalSHA,err:=canonical.Hash(json.RawMessage(r.Proposal));if err!=nil{return zero,err}
 config,err:=canonical.Marshal(map[string]any{"project_id":project,"input_snapshot_id":r.Snapshot,"v28_restoration":map[string]any{"schema_version":1,"tenant_id":tenant,"project_id":project,"snapshot_id":r.Snapshot,"actor_id":who.UserID,"auth_session_id":who.SessionID,"run_id":run,"workflow_id":workflow,"shoot_session_id":r.Shoot,"source_observation_id":r.SourceObservation,"donor_observation_id":nil,"operation":"portrait.glare","ordinal":r.Ordinal,"params_proposal_sha":proposalSHA,"base_asset_version_id":p.Base.AssetVersionID,"donor_asset_version_id":nil,"qualification":s.pin}});if err!=nil{return zero,err}
 tx,err:=s.sourceTx(ctx,who,tenant,project);if err!=nil{return zero,err};var began bool
 err=tx.QueryRow(ctx,"SELECT photo.begin_v28_glare_source_draft($1,$2,$3,$4)",string(config),string(r.Proposal),s.pin.PolicySHA,s.reservedBytes).Scan(&began)
 if err!=nil||!began{tx.Rollback(context.WithoutCancel(ctx));if err==nil{err=v2input.ErrExpired};if ticket,e:=s.sourceTicket(ctx,who,tenant,project,run,workflow,r,profile);e==nil{return ticket,nil};return zero,err}
 if err=tx.Commit(ctx);err!=nil{return zero,err}
 issuer:=v2portrait.OpenEyesConfirmationService{DB:s.scheduler,Objects:s.objects,Bucket:s.bucket}
 params,err:=issuer.IssueGlareSourceParams(ctx,who,tenant,project,r.Shoot,run,r.Proposal,r.SourceObservation);if err!=nil{return zero,err}
 catalog:=SQLArtifactCatalog{DB:s.catalog,Bucket:s.bucket,ProjectID:project,AssetVersionID:p.Base.AssetVersionID}
 material,err:=ResolveRestorationManualEvidence(ctx,s.scheduler,who,tenant,project,r.Snapshot,"portrait.glare",params,catalog,nil,s.objects);if err!=nil{return zero,err}
 publisher:=artifactjson.Publisher{Repository:artifactjson.SQLRepository{DB:s.scheduler,Bucket:s.bucket},Objects:s.objects}
 paramsRef,err:=(SQLRestorationParamsPublisher{DB:s.scheduler,Publisher:publisher}).PublishSourceGlare(ctx,who,tenant,run,workflow,r.Ordinal,params,s.pin,material.Origin);if err!=nil{return zero,err}
 inventory,err:=v2portrait.BuildRestorationInputInventory(ctx,"portrait.glare",params,paramsRef,s.pin,material.Documents);if err!=nil{return zero,err}
 prepared,err:=CompileResolvedRestorationDraftTask(ctx,s.scheduler,who,tenant,project,r.Snapshot,workflow,r.Ordinal,"portrait.glare",params,s.profile,s.pin,paramsRef,inventory,catalog,nil,s.objects);if err!=nil{return zero,err}
 store:=SQLRestorationDescriptorStore{DB:s.scheduler}
 if _,err=store.PublishAndRegisterTransport(ctx,who,tenant,run,prepared,params,publisher);err!=nil{return zero,err}
 if err=store.AdmitRegistered(ctx,who,tenant,run,prepared,params);err!=nil{if ticket,e:=s.sourceTicket(ctx,who,tenant,project,run,workflow,r,profile);e==nil{return ticket,nil};return zero,err}
 return s.sourceTicket(ctx,who,tenant,project,run,workflow,r,profile)
}

func(s *GlareDonorSubmissionService)SourceGlareAvailable(ctx context.Context,who auth.Identity,tenant,project uuid.UUID)(bool,error){
 tx,err:=s.sourceTx(ctx,who,tenant,project);if err!=nil{return false,err};defer tx.Rollback(context.WithoutCancel(ctx))
 if !s.generativeEnabled{return false,nil};pin,err:=s.pin.Compile();if err!=nil{return false,err};var ready bool
 if err=tx.QueryRow(ctx,"SELECT photo.lock_v28_restoration_qualification($1,$2,$3,$4,'portrait.glare',$5::jsonb)",tenant,project,who.UserID,who.SessionID,string(pin)).Scan(&ready);err!=nil{return false,err}
 if err=ctx.Err();err!=nil{return false,err};return ready,nil
}
