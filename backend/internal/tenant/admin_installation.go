package tenant

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// Required before advertising administrative routes: receipt recovery and
// post-wait authority must be installed together, under their original API ACL.
func RequireAdminInstallation(ctx context.Context, db artifactjson.RowQuerier) error {
 if err:=ctx.Err();err!=nil{return err}
 if db==nil{return errors.New("tenant administration database required")}
 bounded,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 var ready bool
 err:=db.QueryRow(bounded,`SELECT current_user='photo_api' AND count(*)=3 AND bool_and(
 p.prosecdef AND p.proretset
 AND l.lanname='plpgsql' AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo'
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.privilege_type='EXECUTE' AND a.grantee NOT IN(p.proowner,'photo_api'::regrole))
 AND (v.marker='' OR strpos(p.prosrc,v.marker)>0))
 FROM (VALUES
 ('photo.read_invitation_receipt(uuid,uuid,uuid,text,text)',''),
 ('photo.create_invitation(uuid,uuid,uuid,uuid,text,text,bytea,timestamp with time zone,uuid,bytea,bytea,text,text,uuid,text,text)','tenant_admin_wait_authority_v1'),
 ('photo.request_tenant_deletion(uuid,uuid,uuid,bigint,uuid)','tenant_admin_wait_authority_v1')) v(signature,marker)
 JOIN pg_proc p ON p.oid=to_regprocedure(v.signature) JOIN pg_language l ON l.oid=p.prolang`).Scan(&ready)
 if err!=nil{return err};if err:=bounded.Err();err!=nil{return err}
 if !ready{return errors.New("tenant administration installation unavailable")};return RequireMemberEmailInstallation(ctx, db)
}
