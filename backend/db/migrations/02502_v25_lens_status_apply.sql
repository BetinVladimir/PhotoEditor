-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_intent_tasks(p_tenant uuid,p_project uuid,p_run uuid,p_actor uuid,p_session uuid,p_after integer)
RETURNS TABLE(ordinal integer,asset_version_id uuid,task_id uuid,state text,result_id uuid,artifact_id uuid,sha256 text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT si.ordinal,si.asset_version_id,t.id,t.state,b.result_id,b.artifact_id,b.sha256
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id AND w.project_id=p_project
 JOIN photo.snapshot_items si ON si.tenant_id=r.tenant_id AND si.snapshot_id=r.input_snapshot_id
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(r.config_snapshot->'v25_lens'->'nodes')='array'
 THEN r.config_snapshot->'v25_lens'->'nodes' ELSE '[]'::jsonb END) n(node)
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.id::text=n.node->>'task_id'
 AND n.node->'params'->'base'->>'asset_version_id'=si.asset_version_id::text
 LEFT JOIN LATERAL photo.v25_adopted_lens_binding(p_tenant,p_project,si.asset_version_id,t.id,p_actor,p_session) b ON true
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2' AND r.graph_sealed
 AND r.created_by=p_actor AND r.config_snapshot->'v25_lens'->>'auth_session_id'=p_session::text
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND t.operation='optical.lens' AND t.operation_version='1' AND p_after>=-1 AND si.ordinal>p_after
 ORDER BY si.ordinal LIMIT 101;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_intent_tasks(uuid,uuid,uuid,uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_intent_tasks(uuid,uuid,uuid,uuid,uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_lens_intent_tasks(uuid,uuid,uuid,uuid,uuid,integer) TO photo_api;
-- +goose StatementBegin
CREATE FUNCTION photo.apply_v25_lens_version(p_tenant uuid,p_project uuid,p_version uuid,p_task uuid,p_actor uuid,p_session uuid,p_decision jsonb,p_root text,p_image text,p_report text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b record;src photo.asset_versions%ROWTYPE;old photo.asset_versions%ROWTYPE;root jsonb;image jsonb;report jsonb;target jsonb;
 h bytea;identity uuid;origin uuid;next_no bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR octet_length(p_root) NOT BETWEEN 2 AND 1048576 OR octet_length(p_image) NOT BETWEEN 2 AND 1048576
 OR octet_length(p_report) NOT BETWEEN 2 AND 1048576 OR p_root IS NULL OR p_image IS NULL OR p_report IS NULL
 OR jsonb_typeof(p_decision) IS DISTINCT FROM 'object' OR (p_decision-ARRAY['schema_version','result_id','root','working_image','confirm_geometry_reset','accept_uncovered_area'])<>'{}'
 OR p_decision->'schema_version' IS DISTINCT FROM '1'::jsonb OR p_decision->'confirm_geometry_reset' IS DISTINCT FROM 'true'::jsonb
 OR jsonb_typeof(p_decision->'accept_uncovered_area') IS DISTINCT FROM 'boolean' THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.sessions WHERE id=p_session AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.users WHERE id=p_actor FOR SHARE;
 PERFORM user_id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project FOR SHARE;
 PERFORM user_id FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=p_project AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 PERFORM r.id FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE t.tenant_id=p_tenant AND t.id=p_task FOR SHARE OF r;
 SELECT * INTO b FROM photo.v25_adopted_lens_binding(p_tenant,p_project,p_version,p_task,p_actor,p_session);
 IF NOT FOUND OR b.result_id::text IS DISTINCT FROM p_decision->>'result_id'
 OR b.artifact_id::text IS DISTINCT FROM p_decision->'root'->>'artifact_id' OR b.sha256 IS DISTINCT FROM p_decision->'root'->>'sha256'
 OR encode(sha256(convert_to(p_root,'UTF8')),'hex') IS DISTINCT FROM b.sha256 THEN RETURN NULL;END IF;
 root:=p_root::jsonb;image:=p_image::jsonb;report:=p_report::jsonb;target:=root->'working_image';
 IF target IS DISTINCT FROM p_decision->'working_image' OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM target->>'sha256'
 OR encode(sha256(convert_to(p_report,'UTF8')),'hex') IS DISTINCT FROM root->'report'->>'sha256'
 OR report->>'operation' IS DISTINCT FROM 'optical.lens' OR coalesce(report->>'status','') NOT IN('pass','review')
 OR (report->>'status'='review' AND p_decision->'accept_uncovered_area' IS DISTINCT FROM 'true'::jsonb) THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v25_adopted_lens_child_object(p_tenant,p_project,p_version,p_task,p_actor,p_session,b.artifact_id,b.sha256,
 (root->'report'->>'artifact_id')::uuid,root->'report'->>'sha256','$/report','ImageReport')) THEN RETURN NULL;END IF;
 SELECT * INTO src FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=p_version AND review_state='accepted';
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.assets WHERE tenant_id=p_tenant AND id=src.asset_id AND project_id=p_project AND deleted_at IS NULL AND upload_state='verified' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF (target->>'artifact_id')::uuid=src.master_artifact_id AND target->>'sha256'=src.content_hash THEN RETURN p_version;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v25_adopted_lens_child_object(p_tenant,p_project,p_version,p_task,p_actor,p_session,b.artifact_id,b.sha256,
 (target->>'artifact_id')::uuid,target->>'sha256','$/working_image','WorkingImage')) THEN RETURN NULL;END IF;
 SELECT run_id INTO origin FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 h:=public.digest(uuid_send(b.result_id)||convert_to('V25.LensAppliedVersion','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);identity:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 SELECT * INTO old FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=identity;
 IF FOUND THEN
 IF old.asset_id=src.asset_id AND old.parent_asset_version_id=p_version AND old.master_artifact_id=(target->>'artifact_id')::uuid
 AND old.content_hash=target->>'sha256' AND old.metadata=image AND old.created_by_run_id=origin
 AND EXISTS(SELECT 1 FROM photo.asset_version_sources WHERE tenant_id=p_tenant AND version_id=identity AND source_version_id=p_version AND role='lens_source' AND ordinal=0)
 THEN RETURN identity;END IF;RETURN NULL;END IF;
 SELECT coalesce(max(version_no),0)::bigint+1 INTO next_no FROM photo.asset_versions WHERE tenant_id=p_tenant AND asset_id=src.asset_id;
 IF next_no>2147483647 THEN RETURN NULL;END IF;
 INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,parent_asset_version_id,created_by_run_id,content_hash,metadata,review_state,version_no)
 VALUES(identity,p_tenant,src.asset_id,(target->>'artifact_id')::uuid,p_version,origin,target->>'sha256',image,'accepted',next_no);
 INSERT INTO photo.asset_version_sources(tenant_id,version_id,source_version_id,role,ordinal) VALUES(p_tenant,identity,p_version,'lens_source',0);
 RETURN identity;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_v25_lens_version(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v25_lens_version(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.apply_v25_lens_version(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text,text,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
