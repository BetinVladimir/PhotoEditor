package collection

import (
 "context"
 "errors"
 "strconv"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/pagination"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

// ListSections includes empty sections and fences every page to its collection
// revision. Callers discard a multi-page read when that revision changes.
func (s Service) ListSections(ctx context.Context, who auth.Identity, tid, collectionID uuid.UUID, limit int, raw string) (pagination.Page[Section], error) {
 out:=pagination.Page[Section]{Items:[]Section{}}
 if limit<1||limit>100{return out,domain.NewError("VALIDATION_FAILED",422)}
 hash,err:=canonical.Hash(map[string]string{"collection_id":collectionID.String()});if err!=nil{return out,err}
 scope:=pagination.Cursor{Tenant:tid.String(),Principal:who.UserID.String(),FilterHash:hash,Sort:"section:position,id"}
 var afterID *uuid.UUID;var afterPosition *int64
 if raw!="" {c,e:=s.Cursors.Decode(raw,scope);if e!=nil{return out,domain.NewError("INVALID_CURSOR",400)};id,e:=uuid.Parse(c.LastID);if e!=nil||id==uuid.Nil{return out,domain.NewError("INVALID_CURSOR",400)};p,e:=strconv.ParseInt(c.LastPosition,10,64);if e!=nil||p<0||p>9007199254740991{return out,domain.NewError("INVALID_CURSOR",400)};afterID,afterPosition=&id,&p}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Viewer,func(tx pgx.Tx,role domain.Role)error{
  // Section writes lock their collection before changing either rows or its
  // revision. Read the revision only after the shared lock is acquired, and
  // repeat project authorization after that potentially blocking wait.
  c,e:=scan(tx.QueryRow(ctx,`SELECT `+fields+` FROM photo.collections c WHERE c.tenant_id=$1 AND c.id=$2 AND c.deleted_at IS NULL FOR SHARE OF c`,tid,collectionID))
  if errors.Is(e,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if e!=nil{return e}
  currentRole,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Viewer);if e!=nil{return e}
  if _,e=project.Authorize(ctx,tx,tid,c.ProjectID,who.UserID,currentRole,domain.Viewer,false);e!=nil{return e};out.Revision=c.Revision
  rows,e:=tx.Query(ctx,`SELECT `+sectionFields+` FROM photo.sections s WHERE s.tenant_id=$1 AND s.collection_id=$2 AND ($3::uuid IS NULL OR (s.position,s.id)>($4::bigint,$3::uuid)) ORDER BY s.position,s.id LIMIT $5`,tid,collectionID,afterID,afterPosition,limit+1);if e!=nil{return e};defer rows.Close()
  for rows.Next(){v,e:=scanSection(rows);if e!=nil{return e};out.Items=append(out.Items,v)};return rows.Err()
 });if err!=nil{return out,err}
 if len(out.Items)>limit{out.Items=out.Items[:limit];last:=out.Items[limit-1];scope.LastID,scope.LastPosition=last.ID.String(),strconv.FormatInt(last.Position,10);next,e:=s.Cursors.Encode(scope);if e!=nil{return out,e};out.NextCursor=&next}
 return out,nil
}

// Accepted receipts and readonly discovery deliberately do not use this gate.
func writableCollectionState(state string)bool{return state=="draft"||state=="published"}
