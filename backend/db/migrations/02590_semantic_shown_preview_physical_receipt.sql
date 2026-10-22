-- +goose Up
-- Physical API acceptance of the selected preview is an immutable receipt,
-- not a generic-adopter inbox. Preserve the shown child's separate inbox proof.
-- +goose StatementBegin
DO $patch$
DECLARE prior record;child record;revised text;
 signature text:='photo.semantic_shown_applied_preview_object(uuid,uuid,uuid,uuid,text,text,text)';
 child_signature text:='photo.semantic_shown_applied_image(uuid,uuid,uuid,uuid)';
 needle text:=$old$IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(preview.id,result.id)) OR NOT EXISTS(SELECT FROM photo.runs WHERE tenant_id=p_tenant AND id=preview.run_id AND status='completed' AND graph_sealed AND NOT cancel_requested) THEN RETURN;END IF;$old$;
 replacement text:=$new$IF NOT FOUND OR NOT EXISTS(
 SELECT FROM photo.runs r
 CROSS JOIN LATERAL (SELECT
  CASE expected
   WHEN 'semantic.preview_local_exposure' THEN r.v27_preapply_preview_receipt
   WHEN 'semantic.preview_remove_object' THEN r.v27_removal_preview_receipt
   WHEN 'semantic.preview_background_cleanup' THEN r.v27_background_cleanup_preview_receipt
   WHEN 'semantic.preview_retouch_region' THEN r.v27_retouch_preview_receipt END AS receipt,
  CASE expected
   WHEN 'semantic.preview_local_exposure' THEN r.config_snapshot->'semantic_preapply_preview'
   WHEN 'semantic.preview_remove_object' THEN r.config_snapshot->'semantic_removal_preview'
   WHEN 'semantic.preview_background_cleanup' THEN r.config_snapshot->'semantic_background_cleanup_preview'
   WHEN 'semantic.preview_retouch_region' THEN r.config_snapshot->'semantic_retouch_preview' END AS config
 ) sealed
 WHERE r.tenant_id=p_tenant AND r.id=preview.run_id AND r.kind='preview'
 AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested
 AND jsonb_typeof(sealed.receipt)='object' AND sealed.receipt->'schema_version'='1'::jsonb
 AND sealed.receipt->'params_sha'=to_jsonb(sealed.config->>'params_sha')
 AND sealed.receipt->'task_id'=to_jsonb(preview.id)
 AND sealed.receipt->'result_id'=to_jsonb(result.id)
 AND sealed.receipt->'attempt'=to_jsonb(preview.attempt)
 AND sealed.receipt->'fence'=to_jsonb(preview.fencing_token)
 AND sealed.receipt->'presentation'=body->'presentation'
 AND sealed.receipt->'working_image'=body->'working_image'
 AND sealed.receipt->'pixels'=body->'pixels'
 AND sealed.receipt->'numerical_evidence'=body->'numerical_evidence'
 AND sealed.config->'params'->>'tenant_id'=p_tenant::text
 AND sealed.config->'params'->>'target_id'=p_target::text
) THEN RETURN;END IF;$new$;
BEGIN
 SELECT p.oid,p.prosrc,pg_get_functiondef(p.oid) definition,p.proowner,p.proacl,p.proconfig,p.prosecdef,
  p.provolatile,p.prorettype,p.proargtypes,p.proargnames,p.proargmodes,p.proallargtypes,p.prokind,p.proretset,p.prolang
 INTO prior FROM pg_proc p WHERE p.oid=to_regprocedure(signature)
  AND p.proowner='photo_fn_owner'::regrole AND p.prosecdef AND p.prokind='f' AND p.proretset
  AND p.prorettype='record'::regtype AND p.pronargs=7
  AND p.proargtypes='2950 2950 2950 2950 25 25 25'::oidvector
  AND p.proargnames=ARRAY['p_tenant','p_target','p_actor','p_session','p_kind','p_params','p_presentation','contract','artifact_id','sha256','bytes','bucket','object_key','object_version','media_type']
  AND pg_get_function_result(p.oid)='TABLE(contract text, artifact_id uuid, sha256 text, bytes bigint, bucket text, object_key text, object_version text, media_type text)'
  AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql') AND p.provolatile='v'
  AND p.proconfig=ARRAY['search_path=pg_catalog, photo']
  AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')='338dae27a3d8a64c72bc35ecde34338e7ffc2d21d62c4249021699884a4b8a1f';
 IF prior.oid IS NULL OR (length(prior.prosrc)-length(replace(prior.prosrc,needle,'')))/length(needle)<>1
  OR strpos(prior.prosrc,replacement)>0 THEN RAISE EXCEPTION 'SHOWN_PREVIEW_RECEIPT_SOURCE_SHAPE';END IF;
 IF EXISTS(SELECT 1 FROM aclexplode(coalesce(prior.proacl,acldefault('f',prior.proowner))) a
   WHERE a.privilege_type='EXECUTE' AND a.grantee NOT IN(prior.proowner,'photo_api'::regrole))
  OR NOT has_function_privilege('photo_api',prior.oid,'EXECUTE')
  OR EXISTS(SELECT 1 FROM pg_roles role WHERE NOT role.rolsuper AND role.oid<>prior.proowner
   AND NOT pg_has_role(role.oid,prior.proowner,'USAGE') AND NOT pg_has_role(role.oid,'photo_api','USAGE')
   AND has_function_privilege(role.oid,prior.oid,'EXECUTE'))
 THEN RAISE EXCEPTION 'SHOWN_PREVIEW_RECEIPT_AUTHORITY_SHAPE';END IF;
 SELECT p.oid,pg_get_functiondef(p.oid) definition,p.proowner,p.proacl,p.proconfig,p.prosecdef,p.provolatile,p.proargnames,p.proargtypes,p.proargmodes,p.proallargtypes
 INTO child FROM pg_proc p WHERE p.oid=to_regprocedure(child_signature)
  AND p.proowner='photo_fn_owner'::regrole AND p.prosecdef AND p.prokind='f' AND p.proretset
  AND p.pronargs=4 AND p.proargtypes='2950 2950 2950 2950'::oidvector
  AND p.prorettype='record'::regtype AND p.provolatile='s'
  AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='sql')
  AND p.proargnames=ARRAY['p_tenant','p_target','p_actor','p_session','child_run_id','task_id','result_id','artifact_id','sha256']
  AND pg_get_function_result(p.oid)='TABLE(child_run_id uuid, task_id uuid, result_id uuid, artifact_id uuid, sha256 text)'
  AND p.proconfig=ARRAY['search_path=pg_catalog, photo']
  AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')='52270a5d9ece58c1037a6303b8f24b2118dee45d9e3b4ec42118d1ec2db4d234';
 IF child.oid IS NULL OR strpos(child.definition,'photo.v2_adoption_event_id(t.id,z.id)')=0
  THEN RAISE EXCEPTION 'SHOWN_PREVIEW_CHILD_PROOF_SHAPE';END IF;
 IF EXISTS(SELECT 1 FROM aclexplode(coalesce(child.proacl,acldefault('f',child.proowner))) a
   WHERE a.privilege_type='EXECUTE' AND a.grantee NOT IN(child.proowner,'photo_api'::regrole))
  OR NOT has_function_privilege('photo_api',child.oid,'EXECUTE')
  OR EXISTS(SELECT 1 FROM pg_roles role WHERE NOT role.rolsuper AND role.oid<>child.proowner
   AND NOT pg_has_role(role.oid,child.proowner,'USAGE') AND NOT pg_has_role(role.oid,'photo_api','USAGE')
   AND has_function_privilege(role.oid,child.oid,'EXECUTE'))
 THEN RAISE EXCEPTION 'SHOWN_PREVIEW_CHILD_AUTHORITY_SHAPE';END IF;
 revised:=replace(prior.prosrc,needle,replacement);
 EXECUTE replace(prior.definition,prior.prosrc,revised);
 IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=prior.oid AND p.prosrc=revised
  AND p.proowner=prior.proowner AND p.proacl IS NOT DISTINCT FROM prior.proacl AND p.proconfig IS NOT DISTINCT FROM prior.proconfig
  AND p.prosecdef=prior.prosecdef AND p.provolatile=prior.provolatile AND p.prorettype=prior.prorettype
  AND p.proargtypes=prior.proargtypes AND p.proargnames IS NOT DISTINCT FROM prior.proargnames
  AND p.proargmodes IS NOT DISTINCT FROM prior.proargmodes AND p.proallargtypes IS NOT DISTINCT FROM prior.proallargtypes
  AND p.prokind=prior.prokind AND p.proretset=prior.proretset AND p.prolang=prior.prolang)
 THEN RAISE EXCEPTION 'SHOWN_PREVIEW_RECEIPT_AUTHORITY_CHANGED';END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=child.oid AND pg_get_functiondef(p.oid)=child.definition
  AND p.proowner=child.proowner AND p.proacl IS NOT DISTINCT FROM child.proacl AND p.proconfig IS NOT DISTINCT FROM child.proconfig
  AND p.prosecdef=child.prosecdef AND p.provolatile=child.provolatile AND p.proargtypes=child.proargtypes
  AND p.proargnames IS NOT DISTINCT FROM child.proargnames AND p.proargmodes IS NOT DISTINCT FROM child.proargmodes
  AND p.proallargtypes IS NOT DISTINCT FROM child.proallargtypes)
 THEN RAISE EXCEPTION 'SHOWN_PREVIEW_CHILD_PROOF_CHANGED';END IF;
END $patch$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
