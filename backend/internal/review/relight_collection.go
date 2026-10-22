package review

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

type RelightCollectionTarget struct {
 Collection uuid.UUID `json:"collection"`
 Item uuid.UUID `json:"item"`
 Asset uuid.UUID `json:"asset"`
 Revision string `json:"revision"`
 ExpectedVersion uuid.UUID `json:"expectedVersion"`
 Name string `json:"name"`
}
type RelightCollectionPage struct {
 Items []RelightCollectionTarget `json:"items"`
 NextCursor *uuid.UUID `json:"next_cursor"`
}

// Discovery is read-only. Each choice captures its current revision; replacement
// uses Collection.ReplaceItemVersion and must never silently refresh that CAS.
func (s *Service) RelightCollectionTargets(ctx context.Context,who auth.Identity,tid,id uuid.UUID,
 decisionHash string,after *uuid.UUID)(RelightCollectionPage,error){
 zero:=RelightCollectionPage{Items:[]RelightCollectionTarget{}}
 selected,err:=s.ReadRestorationResultSelection(ctx,who,tid,id);if err!=nil{return zero,err}
 if selected.DecisionHash!=decisionHash||selected.Route!="candidate"||selected.Image==nil{
  return zero,domain.NewError("STALE_CANDIDATE_REVIEW",409)
 }
 out:=zero
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  var projectID,parent uuid.UUID
  if err:=tx.QueryRow(ctx,`SELECT w.project_id,(v.relight_params_jcs::jsonb->'base'->>'asset_version_id')::uuid
   FROM photo.reviews v JOIN photo.v2_workflows w ON w.tenant_id=v.tenant_id AND w.run_id=v.run_id
   WHERE v.tenant_id=$1 AND v.id=$2 AND v.kind='image.relight' AND v.state='decided'
    AND v.decision_hash=$3`,tid,id,decisionHash).Scan(&projectID,&parent);err!=nil{return err}
  if _,err:=project.Authorize(ctx,tx,tid,projectID,who.UserID,role,domain.Editor,false);err!=nil{return err}
  rows,err:=tx.Query(ctx,`SELECT c.id,i.id,i.asset_id,c.revision::text,i.asset_version_id,c.name
   FROM photo.collection_items i JOIN photo.collections c ON c.tenant_id=i.tenant_id AND c.id=i.collection_id
   JOIN photo.asset_versions version ON version.tenant_id=i.tenant_id AND version.id=i.asset_version_id AND version.asset_id=i.asset_id
   WHERE i.tenant_id=$1 AND c.project_id=$2 AND c.deleted_at IS NULL AND c.state<>'archived'
    AND i.asset_version_id=$3 AND ($4::uuid IS NULL OR i.id>$4)
   ORDER BY i.id LIMIT 51`,tid,projectID,parent,after);if err!=nil{return err};defer rows.Close()
  for rows.Next(){var target RelightCollectionTarget
   if err:=rows.Scan(&target.Collection,&target.Item,&target.Asset,&target.Revision,&target.ExpectedVersion,&target.Name);err!=nil{return err}
   out.Items=append(out.Items,target)
  }
  return rows.Err()
 });if err!=nil{return zero,err}
 if len(out.Items)>50{cursor:=out.Items[49].Item;out.NextCursor=&cursor;out.Items=out.Items[:50]}
 return out,nil
}
