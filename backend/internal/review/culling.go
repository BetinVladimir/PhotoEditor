package review

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "math"
 "sort"
 "strconv"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/pagination"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

type CullingItem struct {
 ItemID uuid.UUID `json:"item_id"`
 AssetID uuid.UUID `json:"asset_id"`
 VersionID uuid.UUID `json:"asset_version_id"`
 SceneID string `json:"scene_id"`
 ExactGroupID string `json:"exact_group_id"`
 BurstGroupID string `json:"burst_group_id"`
 Rank int `json:"rank"`
 Score float64 `json:"score"`
 Recommended bool `json:"recommended"`
 Manual bool `json:"manual"`
 EyeReview bool `json:"eye_review"`
 EyeState string `json:"eye_state"`
}
type cullingProjection struct {Groups ArtifactRef `json:"groups"`;Proposal ArtifactRef `json:"proposal"`;Items []CullingItem `json:"items"`;NeverDelete bool `json:"never_delete"`}
type cullingSelection struct {Selected []uuid.UUID `json:"selected_item_ids"`;Confirmed []uuid.UUID `json:"confirmed_eyes_item_ids"`;NeverDelete bool `json:"never_delete"`}
type CullingDecision struct {ExpectedRevision int64 `json:"expected_revision"`;CandidateHash string `json:"candidate_hash"`;Selected []uuid.UUID `json:"selected_item_ids"`;Confirmed []uuid.UUID `json:"confirmed_eyes_item_ids"`;NeverDelete bool `json:"never_delete"`}
type CullingPage struct {
 ReviewID uuid.UUID `json:"review_id"`;RunID uuid.UUID `json:"run_id"`;Revision int64 `json:"revision"`;State string `json:"state"`;CandidateHash string `json:"candidate_hash"`
 SnapshotID uuid.UUID `json:"snapshot_id"`;SnapshotDigest string `json:"snapshot_digest"`;NeverDelete bool `json:"never_delete"`;Total int `json:"total"`;Items []CullingItem `json:"items"`;NextCursor *string `json:"next_cursor"`
 Groups ArtifactRef `json:"groups"`;Proposal ArtifactRef `json:"proposal"`;Selected []uuid.UUID `json:"selected_item_ids"`;Confirmed []uuid.UUID `json:"confirmed_eyes_item_ids"`
}
type cullingContext struct {View View;Project uuid.UUID;Snapshot uuid.UUID;Digest string;Projection cullingProjection;Selection cullingSelection}
func cullingInvalid()error{return domain.NewError("VALIDATION_FAILED",422)}
func cullingObject(raw []byte,names ...string)bool{
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil||len(fields)!=len(names){return false}
 for _,name:=range names{body,ok:=fields[name];if !ok||bytes.Equal(bytes.TrimSpace(body),[]byte("null")){return false}}
 return true
}
func readCullingProjection(raw json.RawMessage,hash string)(cullingProjection,error){
 var p cullingProjection
 if len(raw)>6<<20||!cullingObject(raw,"groups","proposal","items","never_delete")||decodeExact(raw,&p)!=nil||!p.NeverDelete||!p.Groups.valid()||!p.Proposal.valid()||p.Proposal.SHA256!=hash||len(p.Items)<1||len(p.Items)>5000{return p,cullingInvalid()}
 var fields map[string]json.RawMessage;json.Unmarshal(raw,&fields);var items []json.RawMessage;json.Unmarshal(fields["items"],&items)
 seen:=map[uuid.UUID]bool{}
 for i,item:=range p.Items{
  if !cullingObject(items[i],"item_id","asset_id","asset_version_id","scene_id","exact_group_id","burst_group_id","rank","score","recommended","manual","eye_review","eye_state")||item.ItemID==uuid.Nil||item.AssetID==uuid.Nil||item.VersionID==uuid.Nil||seen[item.ItemID]||item.Rank<1||item.Rank>5000||math.IsNaN(item.Score)||math.IsInf(item.Score,0)||item.Score<0||item.Score>1{return p,cullingInvalid()}
  for _,id:=range []string{item.SceneID,item.ExactGroupID,item.BurstGroupID}{if len(id)<1||len(id)>80{return p,cullingInvalid()}}
  if item.EyeState!="open"&&item.EyeState!="closed"&&item.EyeState!="unknown"{return p,cullingInvalid()};if item.EyeState=="unknown"&&!item.EyeReview{return p,cullingInvalid()};seen[item.ItemID]=true
 }
 return p,nil
}
func sortedCullingIDs(ids []uuid.UUID,minimum int)([]uuid.UUID,error){
 if ids==nil||len(ids)<minimum||len(ids)>5000{return nil,cullingInvalid()};out:=append([]uuid.UUID{},ids...);sort.Slice(out,func(i,j int)bool{return out[i].String()<out[j].String()})
 for i,id:=range out{if id==uuid.Nil||i>0&&id==out[i-1]{return nil,cullingInvalid()}};return out,nil
}
func planCullingDecision(tid,actor uuid.UUID,c cullingContext,req CullingDecision)(cullingSelection,string,error){
 var zero cullingSelection
 if tid==uuid.Nil||actor==uuid.Nil||c.View.ID==uuid.Nil||c.View.RunID==uuid.Nil||c.View.Kind!="culling"||c.Snapshot==uuid.Nil||!c.Projection.NeverDelete||!req.NeverDelete||req.ExpectedRevision<1||req.ExpectedRevision>=maxRevision{return zero,"",cullingInvalid()}
 if c.View.State!="pending"||c.View.Revision!=req.ExpectedRevision||req.CandidateHash!=c.View.CandidateHash{return zero,"",domain.NewError("REVISION_CONFLICT",409)}
 selected,e:=sortedCullingIDs(req.Selected,1);if e!=nil{return zero,"",e};confirmed,e:=sortedCullingIDs(req.Confirmed,0);if e!=nil{return zero,"",e}
 items:=map[uuid.UUID]CullingItem{};for _,item:=range c.Projection.Items{items[item.ItemID]=item};chosen:=map[uuid.UUID]bool{};eyes:=map[uuid.UUID]bool{}
 for _,id:=range selected{if _,ok:=items[id];!ok{return zero,"",cullingInvalid()};chosen[id]=true};for _,id:=range confirmed{if !chosen[id]{return zero,"",cullingInvalid()};eyes[id]=true};for id:=range chosen{if items[id].EyeReview&&!eyes[id]{return zero,"",cullingInvalid()}}
 selection:=cullingSelection{selected,confirmed,true}
 hash,e:=canonical.Hash(map[string]any{"version":"culling_decision_v1","tenant_id":tid,"review_id":c.View.ID,"run_id":c.View.RunID,"actor_id":actor,"candidate_hash":c.View.CandidateHash,"snapshot_id":c.Snapshot,"snapshot_digest":c.Digest,"selected_item_ids":selected,"confirmed_eyes_item_ids":confirmed,"never_delete":true})
 return selection,hash,e
}
func readCullingView(ctx context.Context,tx pgx.Tx,tid,id uuid.UUID)(View,error){
 var v View;e:=tx.QueryRow(ctx,`SELECT id,run_id,kind,state,revision,candidate_hash,decision,decision_route,decision_hash,reasons,created_at FROM photo.reviews WHERE tenant_id=$1 AND id=$2 AND kind='culling'`,tid,id).Scan(&v.ID,&v.RunID,&v.Kind,&v.State,&v.Revision,&v.CandidateHash,&v.Decision,&v.DecisionRoute,&v.DecisionHash,&v.Reasons,&v.CreatedAt);if errors.Is(e,pgx.ErrNoRows){e=domain.NewError("NOT_FOUND",404)};return v,e
}
func loadCullingContext(ctx context.Context,tx pgx.Tx,who auth.Identity,tid,id uuid.UUID,role domain.Role)(cullingContext,error){
 var c cullingContext;var raw,selection json.RawMessage
 e:=tx.QueryRow(ctx,`SELECT coalesce(src.project_id,effects.project_id),c.snapshot_id,c.snapshot_digest,c.projection,v.selection
 FROM photo.apply_culling_reviews c JOIN photo.reviews v ON v.tenant_id=c.tenant_id AND v.id=c.review_id AND v.run_id=c.run_id
 JOIN photo.runs r ON r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.input_snapshot_id=c.snapshot_id
 JOIN photo.snapshots snap ON snap.tenant_id=c.tenant_id AND snap.id=c.snapshot_id AND snap.digest=c.snapshot_digest AND snap.item_count=jsonb_array_length(c.projection->'items')
 LEFT JOIN photo.collections src ON src.tenant_id=snap.tenant_id AND src.id=snap.collection_id
 LEFT JOIN photo.editor_effect_builds effects ON effects.tenant_id=r.tenant_id AND effects.run_id=r.id
 WHERE c.tenant_id=$1 AND c.review_id=$2 AND v.kind='culling' AND v.state IN('pending','decided')
 AND r.config_snapshot->>'input_snapshot_digest'=c.snapshot_digest`,tid,id).Scan(&c.Project,&c.Snapshot,&c.Digest,&raw,&selection)
 if errors.Is(e,pgx.ErrNoRows){return c,domain.NewError("NOT_FOUND",404)};if e!=nil{return c,e}
 if _,e=project.Authorize(ctx,tx,tid,c.Project,who.UserID,role,domain.Editor,false);e!=nil{return c,e}
 if c.View,e=readCullingView(ctx,tx,tid,id);e!=nil{return c,e};if c.Projection,e=readCullingProjection(raw,c.View.CandidateHash);e!=nil{return c,e}
 var live bool;e=tx.QueryRow(ctx,`SELECT count(*)=jsonb_array_length($4::jsonb->'items') AND bool_and(EXISTS(
 SELECT 1 FROM photo.snapshot_items si JOIN photo.asset_versions av ON av.tenant_id=si.tenant_id AND av.id=si.asset_version_id AND av.master_artifact_id=si.artifact_id AND av.content_hash=si.sha256
 JOIN photo.assets a ON a.tenant_id=av.tenant_id AND a.id=av.asset_id AND a.project_id=$3 AND a.deleted_at IS NULL AND a.upload_state='verified'
 JOIN photo.artifacts ar ON ar.tenant_id=si.tenant_id AND ar.id=si.artifact_id AND ar.sha256=si.sha256 AND ar.state='ready' AND ar.verified_at IS NOT NULL AND ar.object_version IS NOT NULL AND ar.object_version NOT IN('','null') AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
 WHERE si.tenant_id=$1 AND si.snapshot_id=$2 AND si.item_id::text=target->>'item_id' AND si.asset_version_id::text=target->>'asset_version_id' AND a.id::text=target->>'asset_id'))
 FROM jsonb_array_elements($4::jsonb->'items') target`,tid,c.Snapshot,c.Project,string(raw)).Scan(&live)
 if e!=nil{return c,e};if !live{return c,domain.NewError("SOURCE_VERSION_MISMATCH",409)}
 if c.View.State=="decided"{if decodeExact(selection,&c.Selection)!=nil||!c.Selection.NeverDelete{return c,domain.NewError("INPUT_INVALID",422)};selected,e:=sortedCullingIDs(c.Selection.Selected,1);if e!=nil{return c,e};confirmed,e:=sortedCullingIDs(c.Selection.Confirmed,0);if e!=nil{return c,e};c.Selection.Selected,c.Selection.Confirmed=selected,confirmed; pending:=c;pending.View.State="pending";pending.View.Revision=1;if _,_,e:=planCullingDecision(tid,who.UserID,pending,CullingDecision{ExpectedRevision:1,CandidateHash:c.View.CandidateHash,Selected:selected,Confirmed:confirmed,NeverDelete:true});e!=nil{return c,domain.NewError("INPUT_INVALID",422)}}
 return c,nil
}
func(s *Service) detailCulling(ctx context.Context,who auth.Identity,tid,id uuid.UUID)(DetailView,bool,error){
 var out DetailView;handled:=false
 e:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  var kind string;e:=tx.QueryRow(ctx,`SELECT kind FROM photo.reviews WHERE tenant_id=$1 AND id=$2`,tid,id).Scan(&kind);if errors.Is(e,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if e!=nil{return e};if kind!="culling"{return nil};handled=true
  c,e:=loadCullingContext(ctx,tx,who,tid,id,role);out.View=c.View;return e
 });return out,handled,e
}
func(s *Service) CullingItems(ctx context.Context,who auth.Identity,tid,id uuid.UUID,after string)(CullingPage,error){
 var out CullingPage;if s==nil||s.DB==nil{return out,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 var c cullingContext;e:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{var e error;c,e=loadCullingContext(ctx,tx,who,tid,id,role);return e});if e!=nil{return out,e}
 hash,e:=canonical.Hash(map[string]any{"review":c.View.ID,"run":c.View.RunID,"candidate":c.View.CandidateHash,"snapshot":c.Snapshot,"digest":c.Digest,"revision":c.View.Revision,"state":c.View.State});if e!=nil{return out,e}
 scope:=pagination.Cursor{Tenant:tid.String(),Principal:who.UserID.String(),FilterHash:hash,Sort:"culling:frozen-ordinal",Revision:strconv.FormatInt(c.View.Revision,10)};start:=0
 if after!=""{cursor,e:=s.Cursors.Decode(after,scope);if e!=nil||cursor.Revision!=scope.Revision{return out,domain.NewError("INVALID_CURSOR",400)};start,e=strconv.Atoi(cursor.LastPosition);if e!=nil||start<1||start>=len(c.Projection.Items)||cursor.LastID!=c.Projection.Items[start-1].ItemID.String(){return out,domain.NewError("INVALID_CURSOR",400)}}
 end:=start+100;if end>len(c.Projection.Items){end=len(c.Projection.Items)}
 out=CullingPage{ReviewID:id,RunID:c.View.RunID,Revision:c.View.Revision,State:c.View.State,CandidateHash:c.View.CandidateHash,SnapshotID:c.Snapshot,SnapshotDigest:c.Digest,NeverDelete:true,Total:len(c.Projection.Items),Items:append([]CullingItem{},c.Projection.Items[start:end]...),Groups:c.Projection.Groups,Proposal:c.Projection.Proposal}
 if c.View.State=="decided"{out.Selected=c.Selection.Selected;out.Confirmed=c.Selection.Confirmed}
 if end<len(c.Projection.Items){scope.LastPosition=strconv.Itoa(end);scope.LastID=c.Projection.Items[end-1].ItemID.String();next,e:=s.Cursors.Encode(scope);if e!=nil{return CullingPage{},domain.NewError("DEPENDENCY_UNAVAILABLE",503)};out.NextCursor=&next};return out,nil
}
func(s *Service) DecideCulling(ctx context.Context,who auth.Identity,tid,id uuid.UUID,key string,req CullingDecision)(idempotency.Response,error){
 var out idempotency.Response;if s==nil||s.DB==nil{return out,domain.NewError("DEPENDENCY_UNAVAILABLE",503)};if !idempotency.ValidKey(key){return out,domain.NewError("IDEMPOTENCY_KEY_REQUIRED",400)}
 e:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  // Authorize the project before allowing a stored response to leave the API.
  c,e:=loadCullingContext(ctx,tx,who,tid,id,role);if e!=nil{return e}
  out,e=idempotency.Use(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Review.Culling."+id.String(),Key:key},req,time.Now().Add(24*time.Hour),func()(idempotency.Response,error){
   selected,hash,e:=planCullingDecision(tid,who.UserID,c,req);if e!=nil{return idempotency.Response{},e};var applied bool
   e=tx.QueryRow(ctx,`SELECT photo.decide_apply_culling_review($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,tid,id,req.ExpectedRevision,req.CandidateHash,who.UserID,who.SessionID,selected.Selected,selected.Confirmed,hash,uuid.New()).Scan(&applied)
   if e!=nil{return idempotency.Response{},e};if !applied{return idempotency.Response{},domain.NewError("REVISION_CONFLICT",409)}
   v,e:=readCullingView(ctx,tx,tid,id);if e!=nil{return idempotency.Response{},e};body,e:=canonical.Marshal(v);return idempotency.Response{Status:200,Body:body,ResourceID:&id},e
  });if e!=nil{return e}
  if _,e=readCullingDecisionReceipt(out,id,c.View.RunID,req);e!=nil{return e}
  currentRole,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e}
  _,e=project.Authorize(ctx,tx,tid,c.Project,who.UserID,currentRole,domain.Editor,false);return e
 });var pg *pgconn.PgError;if errors.As(e,&pg){switch pg.Code{case "42501":e=domain.NewError("FORBIDDEN",403);case "22023":e=cullingInvalid();case "40001":e=domain.NewError("REVISION_CONFLICT",409)}};return out,e
}

func readCullingDecisionReceipt(response idempotency.Response,id,run uuid.UUID,req CullingDecision)(View,error){
 var v View
 if response.Status!=200||response.ResourceID==nil||*response.ResourceID!=id||json.Unmarshal(response.Body,&v)!=nil||v.ID!=id||v.RunID!=run||v.Kind!="culling"||v.State!="decided"||req.ExpectedRevision<1||req.ExpectedRevision>=maxRevision||v.Revision!=req.ExpectedRevision+1||v.CandidateHash!=req.CandidateHash||v.Decision==nil||*v.Decision!="accept"||v.DecisionRoute==nil||*v.DecisionRoute!="selection"||v.DecisionHash==nil||v.CreatedAt.IsZero(){return View{},domain.NewError("CULLING_RECEIPT_INVALID",409)}
 if _,e:=domain.ParseDigest(*v.DecisionHash);e!=nil{return View{},domain.NewError("CULLING_RECEIPT_INVALID",409)}
 expected,e:=canonical.Marshal(v);if e!=nil{return View{},e};actual,e:=canonical.Bytes(response.Body);if e!=nil{return View{},e};if !bytes.Equal(expected,actual){return View{},domain.NewError("CULLING_RECEIPT_INVALID",409)};return v,nil
}
