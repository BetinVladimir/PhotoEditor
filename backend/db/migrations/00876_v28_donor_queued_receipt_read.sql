-- +goose Up
-- Historical admission recovery only; never reactivates or publishes work.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_donor_queued_receipt(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;receipt jsonb;plan jsonb;body jsonb;c jsonb;proof text;d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND created_by=p_actor
 AND kind='v2' AND graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.v28_donor_queued_receipt IS NULL OR r.v28_donor_descriptor_jcs IS NULL THEN RETURN NULL;END IF;
 receipt:=r.v28_donor_queued_receipt;plan:=r.v28_donor_descriptor_jcs::jsonb;body:=plan->'params';
 IF receipt->'schema_version' IS DISTINCT FROM '1'::jsonb OR receipt->>'state' IS DISTINCT FROM 'queued'
 OR receipt->>'run_id' IS DISTINCT FROM p_run::text OR receipt->>'actor_id' IS DISTINCT FROM p_actor::text
 OR receipt->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR receipt->>'task_id' IS DISTINCT FROM plan->'task'->>'id'
 OR receipt->'qualification' IS DISTINCT FROM plan->'task'->'execution_profile'->'donor_qualification'
 OR receipt->>'descriptor_sha' IS DISTINCT FROM encode(sha256(convert_to(r.v28_donor_descriptor_jcs,'UTF8')),'hex') THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND workflow_type='preview'
 AND renderer_version='v2.2' AND input_snapshot_id=r.input_snapshot_id FOR SHARE;
 IF NOT FOUND OR receipt->>'workflow_id' IS DISTINCT FROM w.id::text THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(receipt->>'task_id')::uuid FOR SHARE;
 IF NOT FOUND OR t.operation IS DISTINCT FROM 'portrait.open_eyes' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.cancel_requested
 OR t.params_artifact_id::text IS DISTINCT FROM plan->'task'->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM plan->'task'->>'input_digest'
 OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM plan->'task'->'execution_profile'
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF photo.v28_donor_inventory_digest_ready(t.id,t.params_artifact_id,plan->'task'->>'params_sha',t.operation,t.input_digest,plan->'entries') IS DISTINCT FROM true
 OR plan->'entries' IS DISTINCT FROM (SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',artifact_id,'sha256',sha256),'contract',contract) ORDER BY ordinal)
 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=t.id AND contract NOT IN('InputClosureChunk','InputClosureManifest')) THEN RETURN NULL;END IF;
 SELECT doc.* INTO d FROM photo.backend_documents doc JOIN photo.artifacts a ON a.tenant_id=doc.tenant_id AND a.id=doc.artifact_id
 WHERE doc.tenant_id=p_tenant AND doc.run_id=p_run AND doc.purpose='v28_donor_confirmation' AND doc.state='ready'
 AND doc.artifact_id=(body->'confirmation'->>'artifact_id')::uuid AND doc.sha256=body->'confirmation'->>'sha256'
 AND a.state='ready' AND a.sha256=doc.sha256 AND a.bytes=doc.bytes AND a.bucket=doc.bucket
 AND a.media_type='application/json' AND a.output_role=doc.purpose AND a.verified_at IS NOT NULL
 AND a.object_version=doc.object_version AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||doc.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF doc,a;
 IF NOT FOUND THEN RETURN NULL;END IF;
 c:=d.v28_donor_issuer_context;proof:=c->>'proof_jcs';
 IF c->>'actor_id' IS DISTINCT FROM p_actor::text OR c->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR c->>'project_id' IS DISTINCT FROM w.project_id::text OR c->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR c->>'run_id' IS DISTINCT FROM p_run::text OR (c->'params'-'confirmation') IS DISTINCT FROM (body-'confirmation')
 OR proof IS NULL OR octet_length(proof) NOT BETWEEN 2 AND 65536 OR d.bytes IS DISTINCT FROM octet_length(proof)::bigint
 OR encode(sha256(convert_to(proof,'UTF8')),'hex') IS DISTINCT FROM d.sha256 THEN RETURN NULL;END IF;
 IF photo.lock_v28_donor_session(p_tenant,w.project_id,(c->>'shoot_session_id')::uuid,p_actor,p_session,
 proof::jsonb,body->'source_faces',body->'donor_faces') IS DISTINCT FROM true
 OR photo.lock_v28_donor_qualification_objects(p_tenant,w.project_id,p_actor,p_session,receipt->'qualification',plan->'qualification_objects') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN receipt;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v28_donor_queued_receipt(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_donor_queued_receipt(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_donor_queued_receipt(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
