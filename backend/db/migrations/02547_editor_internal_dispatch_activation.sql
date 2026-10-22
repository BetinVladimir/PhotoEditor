-- +goose Up
-- Separate deployment activation from installed runtime qualification.
-- Mounting checks only installed authority, never the activation flag.
-- +goose StatementBegin
DO $$ DECLARE entry record; definition text; source text; revised text; BEGIN
 FOR entry IN SELECT * FROM (VALUES('photo.tenant_capabilities()'),('photo.claim_task(uuid,integer,uuid,jsonb)'),('photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)')) e(signature) LOOP
 SELECT p.prosrc,pg_get_functiondef(p.oid) INTO source,definition FROM pg_proc p
 WHERE p.oid=to_regprocedure(entry.signature) AND p.proowner='photo_fn_owner'::regrole AND p.prosecdef;
 IF source IS NULL OR strpos(source,'''editor.effects_seal''')>0
 OR (length(source)-length(replace(source,'''layer.compose'',''session.describe''','')))/length('''layer.compose'',''session.describe''')<>1 THEN RAISE EXCEPTION 'EDITOR_ACTIVATION_LIST_SHAPE';END IF;
 revised:=replace(source,'''layer.compose'',''session.describe''','''layer.compose'',''editor.effects_seal'',''editor.layers_precompose'',''session.describe''');
 IF (length(revised)-length(replace(revised,'''capture.assess'',''identity.compare''','')))/length('''capture.assess'',''identity.compare''')<>1 THEN RAISE EXCEPTION 'CAPTURE_PREPARE_ACTIVATION_LIST_SHAPE';END IF;
 revised:=replace(revised,'''capture.assess'',''identity.compare''','''capture.prepare'',''capture.assess'',''identity.compare''');
 EXECUTE replace(definition,source,revised);
 END LOOP;
END $$;
CREATE FUNCTION photo.editor_internal_dispatch_installed(p_operation text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_operation IN('editor.effects_seal','editor.layers_precompose')
 AND EXISTS(SELECT FROM photo.operation_registry o WHERE o.operation=p_operation AND o.version='1'
 AND o.resource_class='cpu' AND o.model_id IS NULL AND o.backends='["cpu"]'::jsonb AND o.timeout_seconds=300
 AND o.routing_key=CASE p_operation WHEN 'editor.effects_seal' THEN 'cpu.editor_effects_seal' ELSE 'cpu.editor_layers_precompose' END)
 AND (SELECT count(*)=7 FROM (VALUES
 ('editor.effects_seal','changed_pixels','["application/octet-stream"]'::jsonb,800008192::bigint,1),
 ('editor.effects_seal','generated_pixels','["application/octet-stream"]'::jsonb,800008192::bigint,1),
 ('editor.effects_seal','changed_mask','["application/json"]'::jsonb,2097152::bigint,1),
 ('editor.effects_seal','generated_mask','["application/json"]'::jsonb,2097152::bigint,1),
 ('editor.effects_seal','image_report','["application/json"]'::jsonb,2097152::bigint,1),
 ('editor.effects_seal','image_result','["application/json"]'::jsonb,2097152::bigint,1),
 ('editor.effects_seal','executed_base_receipt','["application/json"]'::jsonb,2097152::bigint,1),
 ('editor.layers_precompose','pixels','["application/x-npy"]'::jsonb,2400008192::bigint,1),
 ('editor.layers_precompose','alpha','["application/x-npy"]'::jsonb,800008192::bigint,1),
 ('editor.layers_precompose','mask_pixels','["application/x-npy"]'::jsonb,800008192::bigint,2),
 ('editor.layers_precompose','mask','["application/json"]'::jsonb,2097152::bigint,2),
 ('editor.layers_precompose','working_image','["application/json"]'::jsonb,2097152::bigint,1),
 ('editor.layers_precompose','image_report','["application/json"]'::jsonb,2097152::bigint,1),
 ('editor.layers_precompose','image_result','["application/json"]'::jsonb,2097152::bigint,1)
 ) expected(operation,role,media_types,max_bytes,max_count) JOIN photo.operation_output_policies p
 ON p.operation=expected.operation AND p.version='1' AND p.role=expected.role
 AND p.media_types=expected.media_types AND p.max_bytes=expected.max_bytes AND p.max_count=expected.max_count
 WHERE expected.operation=p_operation)
 AND (SELECT count(*)=cardinality(required.signatures) AND bool_and(p.proowner='photo_fn_owner'::regrole AND p.prosecdef
 AND EXISTS(SELECT FROM unnest(p.proconfig) setting WHERE replace(setting,' ','')='search_path=pg_catalog,photo')
 AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
 AND CASE WHEN signature LIKE '%editor_effect_seal_adoption_scope(%' THEN
 NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee<>p.proowner)
 ELSE has_function_privilege(CASE WHEN signature LIKE '%worker_ready(%' THEN 'photo_worker' ELSE 'photo_scheduler' END,p.oid,'EXECUTE') END)
 FROM unnest(required.signatures) signature JOIN pg_proc p ON p.oid=to_regprocedure(signature))
 FROM (SELECT CASE p_operation WHEN 'editor.effects_seal' THEN ARRAY[
 'photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
 'photo.editor_effect_seal_adoption_scope(uuid,uuid,integer,bigint,uuid)',
 'photo.adopt_editor_effect_seal(uuid,uuid,integer,bigint,uuid,jsonb,text)',
 'photo.materialize_editor_effect_seal(uuid,uuid,jsonb)'] ELSE ARRAY[
 'photo.editor_layers_precompose_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
 'photo.record_editor_precompose(uuid,uuid,uuid,uuid,text,text)'] END AS signatures) required
$$;
ALTER FUNCTION photo.editor_internal_dispatch_installed(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_internal_dispatch_installed(text) FROM PUBLIC;
CREATE FUNCTION photo.configure_editor_internal_dispatch(p_tenant uuid,p_worker uuid,p_operation text,p_enable boolean) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE worker photo.worker_registrations%ROWTYPE; node photo.resource_nodes%ROWTYPE; changed integer;
BEGIN
 IF p_enable IS NULL OR p_operation IS NULL OR p_operation NOT IN('editor.effects_seal','editor.layers_precompose') THEN RETURN false;END IF;
 IF p_enable THEN
  IF p_tenant IS NULL OR p_worker IS NULL OR photo.editor_internal_dispatch_installed(p_operation) IS DISTINCT FROM true THEN RETURN false;END IF;
  PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
  SELECT * INTO worker FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker FOR SHARE;
  IF NOT FOUND OR worker.enabled IS DISTINCT FROM true OR worker.execution_pool IS DISTINCT FROM 'general' OR photo.capacity_policy_worker_fresh(worker) IS DISTINCT FROM true
  OR (worker.resource_classes ? 'cpu') IS DISTINCT FROM true OR (worker.backends ? 'cpu') IS DISTINCT FROM true OR ((worker.qualified_operations->p_operation) ? 'cpu') IS DISTINCT FROM true THEN RETURN false;END IF;
  SELECT * INTO node FROM photo.resource_nodes WHERE node_id=worker.node_id FOR SHARE;
  IF NOT FOUND OR node.enabled IS DISTINCT FROM true OR photo.capacity_policy_node_fresh(node) IS DISTINCT FROM true
  OR photo.worker_authorized(p_tenant,p_worker,'cpu',worker.node_id,p_operation,'cpu',NULL) IS DISTINCT FROM true THEN RETURN false;END IF;
 END IF;
 IF p_enable AND (photo.capacity_policy_worker_fresh(worker) IS DISTINCT FROM true OR photo.capacity_policy_node_fresh(node) IS DISTINCT FROM true
 OR photo.editor_internal_dispatch_installed(p_operation) IS DISTINCT FROM true) THEN RETURN false;END IF;
 UPDATE photo.operation_registry SET v2_enabled=p_enable WHERE operation=p_operation AND version='1' AND resource_class='cpu' AND backends='["cpu"]'::jsonb;
 GET DIAGNOSTICS changed=ROW_COUNT;RETURN changed=1;
END $$;
ALTER FUNCTION photo.configure_editor_internal_dispatch(uuid,uuid,text,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.configure_editor_internal_dispatch(uuid,uuid,text,boolean) FROM PUBLIC;
DO $$ DECLARE entry record; BEGIN
 FOR entry IN SELECT p.oid,a.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE p.oid IN('photo.editor_internal_dispatch_installed(text)'::regprocedure,'photo.configure_editor_internal_dispatch(uuid,uuid,text,boolean)'::regprocedure)
 AND a.grantee<>0 AND a.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',entry.oid::regprocedure,pg_get_userbyid(entry.grantee));END LOOP;
END $$;
GRANT EXECUTE ON FUNCTION photo.editor_internal_dispatch_installed(text) TO photo_worker,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.configure_editor_internal_dispatch(uuid,uuid,text,boolean) TO photo_maintenance;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
