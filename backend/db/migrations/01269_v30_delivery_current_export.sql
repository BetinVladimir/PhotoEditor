-- +goose Up
-- Private current child observation for independently reconstructed frozen requests.
-- Does not establish parent source/worker lease authority by itself.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_current_export_observation(p_tenant uuid,p_run uuid,p_package uuid,p_request jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;r photo.runs%ROWTYPE;x photo.exports%ROWTYPE;parent photo.runs%ROWTYPE;package photo.delivery_packages%ROWTYPE;workflow photo.v2_workflows%ROWTYPE;slot integer;
 a photo.artifacts%ROWTYPE;t photo.tasks%ROWTYPE;ready boolean;
BEGIN
 request:=p_request;
 IF jsonb_typeof(request) IS DISTINCT FROM 'object' OR request->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR request->>'parent_run_id' IS DISTINCT FROM p_run::text OR coalesce(request->>'slot_index','') !~ '^(0|[1-9][0-9]{0,4})$' THEN RETURN NULL;END IF;
 slot:=(request->>'slot_index')::integer;
 IF slot>79999 OR request->>'child_run_id' IS DISTINCT FROM photo.v30_uuid5(p_run,'child:export:'||slot::text)::text THEN RETURN NULL;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 SELECT * INTO package FROM photo.delivery_packages WHERE tenant_id=p_tenant AND id=p_package AND workflow_id=workflow.id FOR SHARE;
 IF parent.id IS NULL OR parent.kind<>'v2' OR parent.cancel_requested OR parent.status NOT IN('queued','running')
 OR workflow.id IS NULL OR workflow.workflow_type<>'delivery' OR package.id IS NULL OR package.state<>'building' THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=(request->>'child_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR r.kind<>'v2' OR r.created_by IS DISTINCT FROM parent.created_by OR r.input_snapshot_id IS DISTINCT FROM parent.input_snapshot_id
 OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM request
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows cw WHERE cw.tenant_id=p_tenant AND cw.run_id=r.id
 AND cw.project_id=workflow.project_id AND cw.input_snapshot_id=workflow.input_snapshot_id AND cw.workflow_type='delivery') THEN RETURN NULL;END IF;
 SELECT * INTO x FROM photo.exports WHERE tenant_id=p_tenant AND id=(r.config_snapshot->>'export_id')::uuid FOR SHARE;
 IF NOT FOUND OR x.asset_version_id::text IS DISTINCT FROM request->'binding'->>'asset_version_id'
 OR x.preset_hash IS DISTINCT FROM request->'preset'->>'preset_sha'
 OR x.id<>photo.v30_uuid5(r.id,'v30:delivery-export:1') THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.delivery_items i JOIN photo.delivery_packages d ON d.tenant_id=i.tenant_id AND d.id=i.package_id
 JOIN photo.v2_workflows w ON w.tenant_id=d.tenant_id AND w.id=d.workflow_id AND w.run_id=p_run
 WHERE i.tenant_id=p_tenant AND i.export_id=x.id AND i.ordinal=(request->>'ordinal')::integer
 AND i.preset_id=(request->'preset'->>'preset_id')::uuid AND i.preset_sha=request->'preset'->>'preset_sha'
 AND d.id=p_package AND i.required IS NOT DISTINCT FROM (request->'preset'->>'required')::boolean AND d.state='building') THEN RETURN NULL;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=x.artifact_id FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=x.task_id;
 ready:=coalesce(x.status='ready' AND a.id IS NOT NULL AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.output_role='encoded_image'
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND t.operation='export.encode' AND t.state='succeeded' AND NOT t.cancel_requested
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)),false);
 RETURN jsonb_build_object('export_id',x.id,'snapshot_digest',request->>'snapshot_digest','binding',request->'binding',
 'preset_sha',request->'preset'->>'preset_sha','run_state',r.status,'export_state',x.status,'artifact_ready',ready,
 'artifact',CASE WHEN ready THEN jsonb_build_object('artifact_id',a.id,'sha256',a.sha256) ELSE NULL END,
 'reason',CASE WHEN r.status='cancelled' THEN 'EXPORT_CANCELLED' WHEN r.status='failed' THEN 'EXPORT_FAILED' ELSE '' END);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_current_export_observation(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_current_export_observation(uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler,photo_worker,photo_api,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
