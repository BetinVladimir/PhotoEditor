-- +goose Up
-- Exact preliminary auto draft recovery; never creates or repairs missing state.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_auto_perspective_replacement_draft(p_tenant uuid,p_expected text,p_segments jsonb,p_bucket text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE packet jsonb;d jsonb;identity jsonb;expected_config jsonb;result text;
BEGIN
 IF photo.lock_v29_auto_perspective_replacement_identity(p_tenant,p_expected,p_segments,p_bucket) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 packet:=p_expected::jsonb;d:=packet->'draft';identity:=packet->'identity';
 IF photo.lock_v29_perspective_qualification(p_tenant,(d->>'project_id')::uuid,(d->>'actor_id')::uuid,
 (d->>'auth_session_id')::uuid,d->'execution_profile'->'perspective_qualification') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 expected_config:=jsonb_build_object('project_id',(d->>'project_id')::uuid,
 'v29_negative_replacement',(packet->>'marker_jcs')::jsonb,
 'v29_negative_replacement_draft_jcs',p_expected,'v29_auto_perspective_segments',p_segments);
 SELECT r.config_snapshot->>'v29_negative_replacement_draft_jcs' INTO result
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=(identity->>'run_id')::uuid
 AND r.created_by=(d->>'actor_id')::uuid AND r.kind='v2' AND r.status='draft'
 AND NOT r.graph_sealed AND NOT r.cancel_requested AND r.input_snapshot_id=(d->>'snapshot_id')::uuid
 AND r.config_snapshot=expected_config AND r.config_hash=encode(sha256(convert_to(expected_config::text,'UTF8')),'hex')
 AND w.id=(identity->>'workflow_id')::uuid AND w.project_id=(d->>'project_id')::uuid
 AND w.input_snapshot_id=r.input_snapshot_id AND w.workflow_type='preview' AND w.renderer_version='v2.2'
 AND w.policy_sha=d->'execution_profile'->'perspective_qualification'->>'policy_sha'
 FOR SHARE OF r,w;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_auto_perspective_replacement_draft(uuid,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_auto_perspective_replacement_draft(uuid,text,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.read_v29_auto_perspective_replacement_draft(uuid,text,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
