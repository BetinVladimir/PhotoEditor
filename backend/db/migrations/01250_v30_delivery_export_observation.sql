-- +goose Up
-- Read relational readiness only; physical verification is an adoption prerequisite.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_export_observation(p_expansion uuid,p_token uuid,p_slot integer) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;x photo.exports%ROWTYPE;
 a photo.artifacts%ROWTYPE;t photo.tasks%ROWTYPE;ready boolean;
BEGIN
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF request IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 IF p_slot>=e.cursor::integer THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(request->>'child_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR r.kind<>'v2' OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM request THEN RETURN NULL;END IF;
 SELECT * INTO x FROM photo.exports WHERE tenant_id=e.tenant_id AND id=(r.config_snapshot->>'export_id')::uuid FOR SHARE;
 IF NOT FOUND OR x.asset_version_id::text IS DISTINCT FROM request->'binding'->>'asset_version_id'
 OR x.preset_hash IS DISTINCT FROM request->'preset'->>'preset_sha'
 OR x.id<>photo.v30_uuid5(r.id,'v30:delivery-export:1') THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.delivery_items i JOIN photo.delivery_packages d ON d.tenant_id=i.tenant_id AND d.id=i.package_id
 JOIN photo.v2_workflows w ON w.tenant_id=d.tenant_id AND w.id=d.workflow_id AND w.run_id=e.run_id
 WHERE i.tenant_id=e.tenant_id AND i.export_id=x.id AND i.ordinal=(request->>'ordinal')::integer
 AND i.preset_id=(request->'preset'->>'preset_id')::uuid AND i.preset_sha=request->'preset'->>'preset_sha'
 AND d.state='building') THEN RETURN NULL;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=e.tenant_id AND id=x.artifact_id FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=x.task_id;
 ready:=coalesce(x.status='ready' AND a.id IS NOT NULL AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.output_role='encoded_image'
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND t.operation='export.encode' AND t.state='succeeded' AND NOT t.cancel_requested
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)),false);
 IF e.expansion_lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('export_id',x.id,'snapshot_digest',request->>'snapshot_digest','binding',request->'binding',
 'preset_sha',request->'preset'->>'preset_sha','run_state',r.status,'export_state',x.status,'artifact_ready',ready,
 'artifact',CASE WHEN ready THEN jsonb_build_object('artifact_id',a.id,'sha256',a.sha256) ELSE NULL END,
 'reason',CASE WHEN r.status='cancelled' THEN 'EXPORT_CANCELLED' WHEN r.status='failed' THEN 'EXPORT_FAILED' ELSE '' END);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_export_observation(uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_export_observation(uuid,uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_export_observation(uuid,uuid,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
