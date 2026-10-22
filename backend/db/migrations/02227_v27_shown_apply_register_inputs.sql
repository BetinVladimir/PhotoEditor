-- +goose Up
-- Private same-transaction complete input registration, following reserved shell.
-- Exact replay allowed; partial/altered registration always fails, never repaired.
-- +goose StatementBegin
CREATE FUNCTION photo.register_semantic_shown_apply_inputs(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE row jsonb;entry jsonb;expected jsonb;actual jsonb;task uuid;parent uuid;total integer:=0;
BEGIN
 PERFORM photo.reserve_semantic_shown_apply_quota(p_tenant,p_project,p_actor,p_child,p_tasks);
 SELECT w.run_id INTO parent FROM photo.v2_review_batches b JOIN photo.v2_workflows w
 ON w.tenant_id=b.tenant_id AND w.id=b.workflow_id
 WHERE b.tenant_id=p_tenant AND b.child_run_id=p_child AND b.project_id=p_project AND b.actor_id=p_actor AND b.action='apply';
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_INPUT_BATCH_INVALID' USING ERRCODE='23514';END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) ORDER BY value->'node'->>'id' LOOP
 task:=(row->'node'->>'id')::uuid;
 IF photo.semantic_shown_apply_input_scope(p_tenant,p_project,task) IS DISTINCT FROM true
 OR photo.semantic_shown_exposure_transport_documents_ready(p_tenant,parent,p_child,(row->>'params_body')::jsonb,
 row->'node',row->'transport',row->'inventory') IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_INPUT_SCOPE_INVALID' USING ERRCODE='23514';END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',ref,'contract',contract,'ordinal',ordinal) ORDER BY ordinal)
 INTO expected FROM (
 SELECT e.value->'ref' AS ref,e.value->>'contract' AS contract,(e.ordinality-1)::integer AS ordinal
 FROM jsonb_array_elements(row->'inventory'->'entries') WITH ORDINALITY e(value,ordinality)
 UNION ALL SELECT row->'transport'->'chunks'->0,'InputClosureChunk',1000000
 UNION ALL SELECT row->'transport'->'manifest','InputClosureManifest',1000001) entries;
 SELECT coalesce(jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),
 'contract',i.contract,'ordinal',i.ordinal) ORDER BY i.ordinal),'[]'::jsonb)
 INTO actual FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=task;
 IF actual<>'[]'::jsonb THEN
 IF actual IS DISTINCT FROM expected OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.task_id=task AND i.project_id IS DISTINCT FROM p_project) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_INPUT_REPLAY_CONFLICT' USING ERRCODE='40001';END IF;
 ELSE
 FOR entry IN SELECT value FROM jsonb_array_elements(expected) LOOP
 INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
 VALUES(p_tenant,p_project,task,(entry->'ref'->>'artifact_id')::uuid,entry->'ref'->>'sha256',entry->>'contract',(entry->>'ordinal')::integer);
 END LOOP;
 END IF;
 total:=total+jsonb_array_length(expected);
 END LOOP;
 RETURN total;
END $$;
ALTER FUNCTION photo.register_semantic_shown_apply_inputs(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_semantic_shown_apply_inputs(uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
