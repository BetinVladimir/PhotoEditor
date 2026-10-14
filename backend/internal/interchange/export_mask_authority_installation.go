package interchange

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)

type exportMaskAuthorityQuery interface{QueryRow(context.Context,string,...any)pgx.Row}

// Run on policy construction before any object read. Exact signatures prevent
// legacy person profile authority from substituting for the consent-aware gate.
func validateExportMaskAuthorityInstallation(ctx context.Context,db exportMaskAuthorityQuery)error{
 if err:=ctx.Err();err!=nil{return err};if db==nil{return v2input.ErrInvalid}
 signatures:=[]string{
  "photo.semantic_proposal_person_qualification_object(uuid,uuid,uuid,uuid,jsonb)",
  "photo.v30_person_profile_consent_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb)",
  "photo.v30_prompt_mask_qualification_object(uuid,uuid,uuid,uuid,jsonb)",
 }
 return validateExportAuthorityFunctions(ctx,db,signatures)
}

func validateExportAuthorityFunctions(ctx context.Context,db exportMaskAuthorityQuery,signatures []string)error{
 if err:=ctx.Err();err!=nil{return err};if db==nil||len(signatures)==0||len(signatures)>32{return v2input.ErrInvalid}
 var installed bool
 err:=db.QueryRow(ctx,`SELECT coalesce(bool_and(
  p.oid IS NOT NULL AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND p.provolatile='s'
  AND EXISTS(SELECT 1 FROM unnest(p.proconfig) setting
   WHERE regexp_replace(setting,'[[:space:]]','','g')='search_path=pg_catalog,photo')
  AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
   WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
  AND has_function_privilege('photo_api',p.oid,'EXECUTE')
  AND has_function_privilege(current_user,p.oid,'EXECUTE')),false)
 FROM unnest($1::text[]) required(signature)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(required.signature)`,signatures).Scan(&installed)
 if err!=nil{return err};if !installed{return v2input.ErrInvalid};return ctx.Err()
}
