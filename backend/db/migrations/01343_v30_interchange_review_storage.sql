-- +goose Up
-- Keep mutable review state on the existing tenant-scoped backend document;
-- v2_workflows itself remains immutable. Existing purge covers this column.
-- +goose StatementBegin
ALTER TABLE photo.backend_documents ADD COLUMN v30_import_review jsonb;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v30_review_shape CHECK
 (v30_import_review IS NULL OR (purpose='task_params' AND jsonb_typeof(v30_import_review)='object'));
ALTER FUNCTION photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb)
 RENAME TO adopt_v30_interchange_import_pre_review;
REVOKE ALL ON FUNCTION photo.adopt_v30_interchange_import_pre_review(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v30_interchange_import(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_inventory jsonb,p_digest text,p_params text,p_root text,p_report text,p_shards jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authority jsonb;root jsonb;source jsonb;mapping jsonb;original jsonb;targets jsonb:='[]'::jsonb;
 review jsonb;ordinal integer:=0;shard text;state text;proposal jsonb;stream uuid;document uuid;workflow uuid;project uuid;d photo.backend_documents%ROWTYPE;
BEGIN
 -- Predecessor locks tenant/run/task, revalidates current metadata and performs
 -- complete artifact adoption. Any later refusal raises, rolling it all back.
 IF photo.adopt_v30_interchange_import_pre_review(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest,p_params,p_root,p_report,p_shards) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT w.id,w.project_id INTO workflow,project FROM photo.tasks t JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND owner_id=p_task AND purpose='task_params'
 AND artifact_id=(SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task) AND state='ready' FOR UPDATE;
 SELECT v30_import_evidence->'sources' INTO source FROM photo.backend_documents WHERE tenant_id=p_tenant AND owner_id=p_task AND purpose='v30_interchange_import_evidence' AND state='ready';
 IF d.id IS NULL OR source IS NULL OR workflow IS NULL THEN RAISE EXCEPTION 'INTERCHANGE_REVIEW_SCOPE_STALE' USING ERRCODE='23514';END IF;
 root:=p_root::jsonb;
 FOR shard IN SELECT value FROM jsonb_array_elements_text(p_shards) LOOP
  FOR mapping IN SELECT value FROM jsonb_array_elements(shard::jsonb->'entries') LOOP
   original:=source->'entries'->ordinal;proposal:=mapping->'proposed_document';stream:=NULL;document:=NULL;
   IF mapping->'entry_id' IS DISTINCT FROM original->'entry_id'
    OR mapping->'asset_version_id' IS DISTINCT FROM original->'asset_version_id' THEN
    RAISE EXCEPTION 'INTERCHANGE_REVIEW_BINDING_CHANGED' USING ERRCODE='23514';END IF;
   IF proposal IS DISTINCT FROM 'null'::jsonb THEN
    IF original->>'state' IS DISTINCT FROM 'adopted' OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs
     WHERE tenant_id=p_tenant AND task_id=p_task AND contract='EditingRecipeV3'
     AND artifact_id=(proposal->>'artifact_id')::uuid AND sha256=proposal->>'sha256') THEN
     RAISE EXCEPTION 'INTERCHANGE_REVIEW_RECIPE_UNDECLARED' USING ERRCODE='23514';END IF;
    state:='proposed';stream:=photo.v30_uuid5(workflow,'interchange:draft-stream:'||(mapping->>'entry_id'));
    document:=photo.v30_uuid5(workflow,'interchange:draft-document:'||(mapping->>'entry_id'));
   ELSIF EXISTS(SELECT 1 FROM jsonb_array_elements(mapping->'fields') f WHERE f->>'status'='rejected') THEN state:='rejected';
   ELSE state:='unsupported';END IF;
   targets:=targets||jsonb_build_array(jsonb_build_object('ordinal',ordinal,'entry_id',mapping->'entry_id','external_id',mapping->'external_id',
    'asset_version_id',mapping->'asset_version_id','state',state,'stream_id',stream,'document_id',document,'recipe',proposal,'fields',mapping->'fields'));
   ordinal:=ordinal+1;
  END LOOP;
 END LOOP;
 IF ordinal<>jsonb_array_length(source->'entries') THEN RAISE EXCEPTION 'INTERCHANGE_REVIEW_INCOMPLETE' USING ERRCODE='23514';END IF;
 review:=jsonb_build_object('schema_version',1,'workflow_id',workflow,'project_id',project,'task_id',p_task,
 'root',jsonb_build_object('artifact_id',(SELECT outputs->0->>'artifact_id' FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result),'sha256',encode(sha256(convert_to(p_root,'UTF8')),'hex')),
 'report',root->'report','targets',targets);
 IF d.v30_import_review IS NOT NULL AND d.v30_import_review IS DISTINCT FROM review THEN RAISE EXCEPTION 'INTERCHANGE_REVIEW_CONFLICT' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET v30_import_review=review WHERE tenant_id=p_tenant AND id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
