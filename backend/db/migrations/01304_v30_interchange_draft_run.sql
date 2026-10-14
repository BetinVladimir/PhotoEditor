-- +goose Up
-- Draft containers only. Snapshot root is attached after physical publication.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v30_interchange_parse_run(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,
 p_config text,p_snapshot text,p_unsigned text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;request jsonb;bundle jsonb;config jsonb;snapshot jsonb;unsigned jsonb;expected jsonb;
 rid uuid;wid uuid;sid uuid;project uuid;root uuid;existing photo.runs%ROWTYPE;sn photo.snapshots%ROWTYPE;w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_config IS NULL OR octet_length(p_config) NOT BETWEEN 2 AND 65536
 OR p_snapshot IS NULL OR octet_length(p_snapshot) NOT BETWEEN 2 AND 65536
 OR p_unsigned IS NULL OR octet_length(p_unsigned) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND route='Interchange.Parse.Pending' AND key=p_key AND request_hash=p_request_sha
 AND response_status IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
 IF saved IS NULL OR saved->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text OR saved->>'actor_id' IS DISTINCT FROM p_actor::text
 OR saved->>'request_sha' IS DISTINCT FROM p_request_sha THEN RETURN false;END IF;
 request:=saved->'request';bundle:=request->'bundle';project:=(request->>'project_id')::uuid;
 IF request->>'direction' IS DISTINCT FROM 'import' OR request->>'format' IS DISTINCT FROM 'photoeditor_bundle'
 OR saved->'evidence'->'ref' IS DISTINCT FROM bundle
 OR photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(bundle->>'artifact_id')::uuid AND a.sha256=bundle->>'sha256'
 AND a.bytes=(saved->'evidence'->>'bytes')::bigint AND a.bytes BETWEEN 1 AND 68719476736
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.media_type='application/zip' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=p_tenant AND l.project_id=project AND l.artifact_id=a.id AND l.sha256=a.sha256) FOR SHARE OF a;
 IF NOT FOUND THEN RETURN false;END IF;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');
 sid:=photo.v30_uuid5(wid,'interchange:artifact-snapshot:1');root:=photo.v30_uuid5(wid,'interchange:artifact-snapshot-document:1');
 unsigned:=p_unsigned::jsonb;snapshot:=p_snapshot::jsonb;config:=p_config::jsonb;
 expected:=jsonb_build_object('schema_version',1,'snapshot_id',sid,'project_id',project,
 'items',jsonb_build_array(jsonb_build_object('ordinal',0,'artifact',bundle,'contract','InterchangeArchive','owner_id',NULL)));
 IF unsigned IS DISTINCT FROM expected OR snapshot-'snapshot_digest' IS DISTINCT FROM expected
 OR snapshot->>'snapshot_digest' IS DISTINCT FROM encode(sha256(convert_to(p_unsigned,'UTF8')),'hex') THEN RETURN false;END IF;
 expected:=jsonb_build_object('schema_version',1,'project_id',project,'snapshot_id',sid,'workflow_type','interchange',
 'request_sha',p_request_sha,'snapshot_digest',snapshot->>'snapshot_digest','snapshot_document',
 jsonb_build_object('artifact_id',root,'sha256',encode(sha256(convert_to(p_snapshot,'UTF8')),'hex')));
 IF config IS DISTINCT FROM expected THEN RETURN false;END IF;
 SELECT * INTO existing FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR UPDATE;
 IF FOUND THEN
  SELECT * INTO sn FROM photo.snapshots WHERE tenant_id=p_tenant AND id=sid;
  SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=rid;
  RETURN existing.kind='v2' AND existing.created_by=p_actor AND existing.status='draft' AND NOT existing.graph_sealed
  AND NOT existing.cancel_requested AND existing.input_snapshot_id=sid AND existing.config_snapshot=config
  AND existing.config_hash=encode(sha256(convert_to(p_config,'UTF8')),'hex')
  AND sn.id IS NOT NULL AND sn.kind='v2' AND sn.digest=snapshot->>'snapshot_digest' AND sn.item_count=1
  AND (sn.expires_at IS NULL OR sn.expires_at>clock_timestamp())
  AND w.id=wid AND w.project_id=project AND w.workflow_type='interchange' AND w.policy_sha=p_request_sha;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.runs WHERE id=rid) OR EXISTS(SELECT 1 FROM photo.snapshots WHERE id=sid) THEN RETURN false;END IF;
 INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at)
 VALUES(sid,p_tenant,'v2','user',p_actor,snapshot->>'snapshot_digest',1,clock_timestamp()+interval '24 hours');
 INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,artifact_id,sha256,payload)
 VALUES(p_tenant,sid,0,(bundle->>'artifact_id')::uuid,bundle->>'sha256',snapshot->'items'->0);
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(rid,p_tenant,'v2','draft',sid,config,encode(sha256(convert_to(p_config,'UTF8')),'hex'),p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(wid,p_tenant,project,rid,sid,'interchange','v2.2',p_request_sha);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.begin_v30_interchange_parse_run(uuid,uuid,uuid,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v30_interchange_parse_run(uuid,uuid,uuid,text,text,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.begin_v30_interchange_parse_run(uuid,uuid,uuid,text,text,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
