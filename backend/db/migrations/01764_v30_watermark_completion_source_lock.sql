-- +goose Up
-- Hold source/authority through completion; NOWAIT prevents lock-order waits.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v30_watermark_retained_source(p_tenant uuid,p_parent uuid,p_actor uuid,p_session uuid,p_preset uuid,p_sha text,p_config text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb;item jsonb;project uuid;
BEGIN
 IF photo.v30_watermark_retained_source_ready(p_tenant,p_parent,p_actor,p_session,p_preset,p_sha,p_config) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT (r.v30_watermark_sources->p_preset::text->>'pin_jcs')::jsonb INTO body FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_parent;
 project:=(body->'scope'->>'project_id')::uuid;
 PERFORM parent.id FROM photo.tenants tenant JOIN photo.runs parent ON parent.tenant_id=tenant.id
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=parent.tenant_id AND workflow.run_id=parent.id
 JOIN photo.projects p ON p.tenant_id=workflow.tenant_id AND p.id=workflow.project_id
 JOIN photo.graph_expansions expansion ON expansion.tenant_id=parent.tenant_id AND expansion.run_id=parent.id AND expansion.kind='v2.delivery/1'
 WHERE tenant.id=p_tenant AND parent.id=p_parent AND p.id=project
 FOR SHARE OF tenant,parent,workflow,p,expansion NOWAIT;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM version.id FROM photo.asset_versions version JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 JOIN photo.artifacts master ON master.tenant_id=version.tenant_id AND master.id=version.master_artifact_id
 WHERE version.tenant_id=p_tenant AND version.id=(body->'binding'->>'asset_version_id')::uuid
 AND asset.id=(body->>'asset_id')::uuid AND master.id=(body->'binding'->'source'->>'artifact_id')::uuid
 FOR SHARE OF version,asset,master NOWAIT;
 IF NOT FOUND THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(body->'arrays') ORDER BY value->'ref'->>'artifact_id' LOOP
  PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(item->'ref'->>'artifact_id')::uuid FOR SHARE NOWAIT;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 PERFORM actor.id FROM photo.users actor JOIN photo.sessions session_row ON session_row.user_id=actor.id AND session_row.id=p_session
 JOIN photo.memberships member ON member.user_id=actor.id AND member.tenant_id=p_tenant
 WHERE actor.id=p_actor AND actor.status='active' AND member.status='active'
 AND session_row.revoked_at IS NULL AND session_row.expires_at>clock_timestamp()
 FOR SHARE OF actor,session_row,member NOWAIT;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM acl.id FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant AND acl.project_id=project AND acl.user_id=p_actor FOR SHARE NOWAIT;
 RETURN photo.v30_watermark_retained_source_ready(p_tenant,p_parent,p_actor,p_session,p_preset,p_sha,p_config);
EXCEPTION WHEN lock_not_available OR invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v30_watermark_retained_source(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v30_watermark_retained_source(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
DO $$
DECLARE definition text;needle text:='photo.v30_watermark_retained_source_ready(';
BEGIN
 definition:=pg_get_functiondef('photo.v30_watermark_encode_completion_ready(uuid,uuid,text)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_COMPLETION_LOCK_DEFINITION_DRIFT';END IF;
 EXECUTE replace(definition,needle,'photo.lock_v30_watermark_retained_source(');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
