-- +goose Up
-- Producer readiness used after adoption, including review/completed packages.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_producer_ready(p_tenant uuid,p_task uuid,p_artifact uuid,p_policy text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;a photo.artifacts%ROWTYPE;e photo.graph_expansions%ROWTYPE;
 proof jsonb;consumer_name text;expected_operation text;
BEGIN
 IF p_policy IS NULL OR p_policy NOT IN('strip','copyright','approved_exif')
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 expected_operation:=CASE p_policy WHEN 'strip' THEN 'export.encode' ELSE 'metadata.write' END;
 consumer_name:=CASE p_policy WHEN 'strip' THEN 'v30-encode-verified' ELSE 'v30-metadata-verified' END;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.operation<>expected_operation OR t.operation_version<>'1' OR t.state<>'succeeded'
 OR t.cancel_requested OR t.canonical_result_id IS NULL
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR r.cancel_requested OR NOT r.graph_sealed
 OR r.status NOT IN('running','waiting_review','completed')
 OR t.id<>photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),expected_operation||':0')
 OR ((r.config_snapshot->'v30_delivery_export'->>'config_jcs')::jsonb)->>'metadata_policy' IS DISTINCT FROM p_policy THEN RETURN false;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact;
 IF NOT FOUND OR a.state<>'ready' OR a.verified_at IS NULL OR a.bytes<1 OR a.output_role<>'encoded_image'
 OR a.created_by_task_id IS DISTINCT FROM t.id OR a.produced_attempt IS DISTINCT FROM t.attempt
 OR a.produced_fence IS DISTINCT FROM t.fencing_token OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results z JOIN photo.output_uploads u
 ON u.tenant_id=z.tenant_id AND u.task_id=z.task_id AND u.attempt=z.attempt AND u.fence=z.fencing_token
 WHERE z.tenant_id=p_tenant AND z.task_id=t.id AND z.id=t.canonical_result_id
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 AND u.artifact_id=a.id AND u.sha256=a.sha256 AND u.role='encoded_image' AND u.state='ready') THEN RETURN false;END IF;
 IF p_policy<>'strip' THEN
  SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=r.id
  AND kind='v30.delivery.metadata/1' AND state='closed';
  IF NOT FOUND OR e.v30_metadata_proof_jcs IS NULL THEN RETURN false;END IF;
  proof:=e.v30_metadata_proof_jcs::jsonb;
  IF proof->>'task_id' IS DISTINCT FROM t.id::text OR proof->>'policy' IS DISTINCT FROM p_policy
  OR proof->'binding' IS DISTINCT FROM r.config_snapshot->'v30_delivery_export'->'binding' THEN RETURN false;END IF;
 END IF;
 RETURN EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer=consumer_name
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_producer_ready(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_producer_ready(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.v30_delivery_export_observation(uuid,uuid,integer)'::regprocedure) IS DISTINCT FROM 'e3b684325bfd82caf16ea84b2e2cd105' THEN RAISE EXCEPTION 'DELIVERY_OBSERVATION_DEFINITION_DRIFT';END IF;END $$;

CREATE OR REPLACE FUNCTION photo.v30_delivery_export_observation(p_expansion uuid,p_token uuid,p_slot integer) RETURNS jsonb
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
 AND photo.v30_delivery_producer_ready(e.tenant_id,t.id,a.id,((request->>'config_jcs')::jsonb)->>'metadata_policy') AND t.state='succeeded' AND NOT t.cancel_requested
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)),false);
 IF e.expansion_lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('export_id',x.id,'snapshot_digest',request->>'snapshot_digest','binding',request->'binding',
 'preset_sha',request->'preset'->>'preset_sha','run_state',r.status,'export_state',x.status,'artifact_ready',ready,
 'artifact',CASE WHEN ready THEN jsonb_build_object('artifact_id',a.id,'sha256',a.sha256) ELSE NULL END,
 'reason',CASE WHEN r.status='cancelled' THEN 'EXPORT_CANCELLED' WHEN r.status='failed' THEN 'EXPORT_FAILED' ELSE '' END);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;

DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.v30_delivery_current_export_observation(uuid,uuid,uuid,jsonb)'::regprocedure) IS DISTINCT FROM '07d551faf083553b7098b7004c70c74a' THEN RAISE EXCEPTION 'DELIVERY_OBSERVATION_DEFINITION_DRIFT';END IF;END $$;

CREATE OR REPLACE FUNCTION photo.v30_delivery_current_export_observation(p_tenant uuid,p_run uuid,p_package uuid,p_request jsonb) RETURNS jsonb
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
 IF parent.id IS NULL OR parent.kind<>'v2' OR parent.cancel_requested OR parent.status NOT IN('queued','running','waiting_review','completed')
 OR workflow.id IS NULL OR workflow.workflow_type<>'delivery' OR package.id IS NULL OR NOT ((parent.status IN('queued','running') AND package.state='building') OR (parent.status='waiting_review' AND package.state='review') OR (parent.status='completed' AND package.state='ready')) THEN RETURN NULL;END IF;
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
 AND d.id=p_package AND i.required IS NOT DISTINCT FROM (request->'preset'->>'required')::boolean AND d.state=package.state) THEN RETURN NULL;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=x.artifact_id FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=x.task_id;
 ready:=coalesce(x.status='ready' AND a.id IS NOT NULL AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.output_role='encoded_image'
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND photo.v30_delivery_producer_ready(p_tenant,t.id,a.id,((request->>'config_jcs')::jsonb)->>'metadata_policy') AND t.state='succeeded' AND NOT t.cancel_requested
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)),false);
 RETURN jsonb_build_object('export_id',x.id,'snapshot_digest',request->>'snapshot_digest','binding',request->'binding',
 'preset_sha',request->'preset'->>'preset_sha','run_state',r.status,'export_state',x.status,'artifact_ready',ready,
 'artifact',CASE WHEN ready THEN jsonb_build_object('artifact_id',a.id,'sha256',a.sha256) ELSE NULL END,
 'reason',CASE WHEN r.status='cancelled' THEN 'EXPORT_CANCELLED' WHEN r.status='failed' THEN 'EXPORT_FAILED' ELSE '' END);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
