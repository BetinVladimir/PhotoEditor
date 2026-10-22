package v2input

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// RequireDraftEdgeUUID refuses the historical six-group prerequisite validator.
// Inspect the actual compiler entrypoint, preserving later queue-specific edits.
func RequireDraftEdgeUUID(ctx context.Context, db artifactjson.RowQuerier) error {
 if err:=ctx.Err();err!=nil{return err}
 if db==nil{return errors.New("draft schema database required")}
 bounded,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 var ready bool
 err:=db.QueryRow(bounded,`SELECT EXISTS(SELECT FROM pg_proc p
 WHERE p.oid=to_regprocedure('photo.create_v2_draft_shell(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint)')
 AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 AND strpos(p.prosrc,$needle$coalesce(edge->>'prerequisite_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'$needle$)>0
 AND strpos(p.prosrc,$needle$coalesce(edge->>'prerequisite_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'$needle$)=0)`).Scan(&ready)
 if err!=nil{return err}
 if err=bounded.Err();err!=nil{return err}
 if !ready{return errors.New("V2 draft dependency UUID schema unavailable")}
 return nil
}
