package review

import (
 "context"
 "errors"
 "encoding/json"
 "strconv"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/pagination"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

// CollectionCullingItem never substitutes a current asset version for a frozen
// recommendation. Historical targets absent from this collection are counted.
type CollectionCullingItem struct {
 CollectionItemID uuid.UUID `json:"collection_item_id"`
 CullingItem
}
type CollectionCullingContext struct {
 ReviewID uuid.UUID `json:"review_id"`
 RunID uuid.UUID `json:"run_id"`
 CandidateHash string `json:"candidate_hash"`
 State string `json:"state"`
 Total int `json:"total"`
 Matched int `json:"matched"`
 Unmatched int `json:"unmatched"`
 Items []CollectionCullingItem `json:"items"`
}
type CollectionCullingPage struct {
 CollectionID uuid.UUID `json:"collection_id"`
 Revision string `json:"revision"`
 Contexts []CollectionCullingContext `json:"contexts"`
 NextCursor *string `json:"next_cursor"`
}
type collectionMember struct {ID,Asset,Version uuid.UUID}
func mapCollectionCulling(c cullingContext,members []collectionMember,include bool)CollectionCullingContext{
 out:=CollectionCullingContext{ReviewID:c.View.ID,RunID:c.View.RunID,CandidateHash:c.View.CandidateHash,State:c.View.State,Total:len(c.Projection.Items)}
 if include{out.Items=[]CollectionCullingItem{}}
 byVersion:=map[[2]uuid.UUID]collectionMember{};for _,member:=range members{byVersion[[2]uuid.UUID{member.Asset,member.Version}]=member}
 for _,item:=range c.Projection.Items{
  member,found:=byVersion[[2]uuid.UUID{item.AssetID,item.VersionID}];if found&&include{out.Items=append(out.Items,CollectionCullingItem{member.ID,item})}
  if found{out.Matched++}else{out.Unmatched++}
 }
 return out
}
// CollectionCullingGroups is read-only. The existing culling source validator is
// reused, including live project/source authority; no client inference creates groups.
func(s *Service) CollectionCullingGroups(ctx context.Context,who auth.Identity,tid,collectionID,reviewID uuid.UUID,after string)(CollectionCullingPage,error){
 out:=CollectionCullingPage{CollectionID:collectionID,Contexts:[]CollectionCullingContext{}}
 if s==nil||s.DB==nil{return out,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 if collectionID==uuid.Nil||len(after)>2048||reviewID!=uuid.Nil&&after!=""{return out,domain.NewError("VALIDATION_FAILED",422)}
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  var projectID uuid.UUID;var revision int64
  e:=tx.QueryRow(ctx,`SELECT project_id,revision FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR SHARE`,tid,collectionID).Scan(&projectID,&revision)
  if errors.Is(e,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if e!=nil{return e}
  if _,e=project.Authorize(ctx,tx,tid,projectID,who.UserID,role,domain.Editor,false);e!=nil{return e};out.Revision=strconv.FormatInt(revision,10)
  hash,e:=canonical.Hash(map[string]any{"collection_id":collectionID,"revision":out.Revision});if e!=nil{return e}
  scope:=pagination.Cursor{Tenant:tid.String(),Principal:who.UserID.String(),FilterHash:hash,Sort:"collection:culling:created_at,id",Revision:out.Revision}
  position:=time.Time{};last:=uuid.Nil
  if after!=""{cursor,e:=s.Cursors.Decode(after,scope);if e!=nil||cursor.Revision!=out.Revision{return domain.NewError("INVALID_CURSOR",400)};position,e=time.Parse(time.RFC3339Nano,cursor.LastPosition);if e!=nil{return domain.NewError("INVALID_CURSOR",400)};last,e=uuid.Parse(cursor.LastID);if e!=nil||last==uuid.Nil{return domain.NewError("INVALID_CURSOR",400)}}
  rows,e:=tx.Query(ctx,`SELECT v.id,v.created_at FROM photo.apply_culling_reviews c
   JOIN photo.reviews v ON v.tenant_id=c.tenant_id AND v.id=c.review_id AND v.run_id=c.run_id AND v.kind='culling' AND v.state IN('pending','decided')
   JOIN photo.runs r ON r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.input_snapshot_id=c.snapshot_id
   JOIN photo.snapshots snap ON snap.tenant_id=c.tenant_id AND snap.id=c.snapshot_id AND snap.digest=c.snapshot_digest
   WHERE c.tenant_id=$1 AND (snap.collection_id=$2 OR r.output_collection_id=$2)
   AND ($3::uuid IS NULL OR v.id=$3) AND (v.created_at,v.id)>($4,$5)
   ORDER BY v.created_at,v.id LIMIT 11`,tid,collectionID,nullableCollectionReview(reviewID),position,last)
  if e!=nil{return e};type candidate struct{id uuid.UUID;created time.Time};candidates:=[]candidate{}
  for rows.Next(){var c candidate;if e=rows.Scan(&c.id,&c.created);e!=nil{rows.Close();return e};candidates=append(candidates,c)};e=rows.Err();rows.Close();if e!=nil{return e}
  if reviewID!=uuid.Nil&&len(candidates)!=1{return domain.NewError("NOT_FOUND",404)}
  more:=len(candidates)>10;if more{candidates=candidates[:10]}
  for _,candidate:=range candidates{
   c,e:=loadCullingContext(ctx,tx,who,tid,candidate.id,role);if e!=nil{return e};if c.Project!=projectID{return domain.NewError("NOT_FOUND",404)}
   // Fetch only validated frozen targets; unrelated members have no size cap.
   targets,e:=json.Marshal(c.Projection.Items);if e!=nil{return e}
   members,e:=collectionCullingMembers(ctx,tx,tid,collectionID,targets);if e!=nil{return e}
   out.Contexts=append(out.Contexts,mapCollectionCulling(c,members,reviewID!=uuid.Nil))
  }
  if more{c:=candidates[len(candidates)-1];scope.LastPosition=c.created.UTC().Format(time.RFC3339Nano);scope.LastID=c.id.String();next,e:=s.Cursors.Encode(scope);if e!=nil{return e};out.NextCursor=&next}
  currentRole,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e}
  if _,e=project.Authorize(ctx,tx,tid,projectID,who.UserID,currentRole,domain.Editor,false);e!=nil{return e}
  return ctx.Err()
 });return out,err
}
func nullableCollectionReview(id uuid.UUID)any{if id==uuid.Nil{return nil};return id}

func collectionCullingMembers(ctx context.Context,tx pgx.Tx,tid,collectionID uuid.UUID,targets json.RawMessage)([]collectionMember,error){
 members:=[]collectionMember{}
 rows,e:=tx.Query(ctx,`SELECT ci.id,ci.asset_id,ci.asset_version_id
 FROM photo.collection_items ci
 JOIN jsonb_to_recordset($3::jsonb) AS target(asset_id uuid,asset_version_id uuid)
 ON target.asset_id=ci.asset_id AND target.asset_version_id=ci.asset_version_id
 WHERE ci.tenant_id=$1 AND ci.collection_id=$2 ORDER BY ci.position,ci.id`,tid,collectionID,string(targets))
 if e!=nil{return nil,e};defer rows.Close()
 for rows.Next(){var m collectionMember;if e=rows.Scan(&m.ID,&m.Asset,&m.Version);e!=nil{return nil,e};members=append(members,m)}
 return members,rows.Err()
}
