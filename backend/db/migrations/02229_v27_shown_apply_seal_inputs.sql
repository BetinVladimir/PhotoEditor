-- +goose Up
-- Seal exact registered inputs, leaving every task blocked and child draft.
-- Caller must activate/finalize in same transaction or roll everything back.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_semantic_shown_apply_inputs(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE registered integer;expected integer;row jsonb;sealed_profile jsonb;sealed_inputs jsonb;sealed integer:=0;
BEGIN
 registered:=photo.register_semantic_shown_apply_inputs(p_tenant,p_project,p_actor,p_child,p_tasks);
 SELECT sum(jsonb_array_length(x.value->'inventory'->'entries')+2)::integer INTO expected
 FROM jsonb_array_elements(p_tasks) AS x(value);
 IF registered IS DISTINCT FROM expected THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_INPUTS_INCOMPLETE' USING ERRCODE='23514';END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) ORDER BY value->'node'->>'id' LOOP
 sealed_inputs:=jsonb_build_array(row->'transport'->'manifest')||(row->'transport'->'direct_roots');
 sealed_profile:=(row->'node'->'execution_profile')||jsonb_build_object(
 'renderer_version','v2.2','contracts_sha','13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5',
 'v2_input_manifest',row->'transport'->'manifest','v2_entry_count',(row->'transport'->>'entry_count')::integer,
 'v2_entries_sha',row->'transport'->>'entries_sha');
 UPDATE photo.tasks SET inputs=sealed_inputs,execution_profile=sealed_profile
 WHERE tenant_id=p_tenant AND run_id=p_child AND id=(row->'node'->>'id')::uuid
 AND state='blocked' AND attempt=0 AND NOT cancel_requested AND inputs='[]'::jsonb
 AND execution_profile=row->'node'->'execution_profile';
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_SEAL_STALE' USING ERRCODE='40001';END IF;
 sealed:=sealed+1;
 END LOOP;
 IF sealed IS DISTINCT FROM jsonb_array_length(p_tasks)
 OR photo.semantic_shown_apply_sealed_inputs_ready(p_tenant,p_project,p_actor,p_child,p_tasks) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_SEAL_INCOMPLETE' USING ERRCODE='23514';END IF;
 RETURN sealed;
END $$;
ALTER FUNCTION photo.seal_semantic_shown_apply_inputs(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_shown_apply_inputs(uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
