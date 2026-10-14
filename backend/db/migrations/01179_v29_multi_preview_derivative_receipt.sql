-- +goose Up
ALTER TABLE photo.backend_documents ADD COLUMN v29_multi_preview_receipt jsonb;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v29_multi_preview_receipt CHECK(v29_multi_preview_receipt IS NULL OR
 (purpose='task_params' AND v29_multi_preview_params IS NOT NULL AND jsonb_typeof(v29_multi_preview_receipt)='object' AND octet_length(v29_multi_preview_receipt::text)<=2097152));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_multi_preview_receipt() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.v29_multi_preview_receipt IS DISTINCT FROM OLD.v29_multi_preview_receipt AND
 (current_user<>'photo_fn_owner' OR OLD.v29_multi_preview_receipt IS NOT NULL) THEN
 RAISE EXCEPTION 'Derivative receipt is immutable' USING ERRCODE='42501';END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v29_multi_preview_receipt() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_multi_preview_receipt() FROM PUBLIC;
CREATE TRIGGER backend_documents_v29_multi_preview_receipt_guard BEFORE UPDATE OF v29_multi_preview_receipt ON photo.backend_documents
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_multi_preview_receipt();
-- +goose StatementBegin
CREATE FUNCTION photo.record_v29_multi_preview_receipt(p_tenant uuid,p_run uuid,p_variant text,p_result uuid,p_attempt integer,p_fence bigint,p_manifest text,p_locations jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;s record;o record;d photo.backend_documents%ROWTYPE;m jsonb;params jsonb;
 manifest_id uuid;manifest_sha text;derivative_id uuid;derivative_sha text;mime text;objects jsonb:='[]';receipt jsonb;count_rows integer:=0; witness jsonb; current_artifact photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 THEN RETURN false;END IF;
 IF jsonb_typeof(p_locations) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_locations)<>4 THEN RETURN false;END IF;
 IF p_manifest IS NULL OR octet_length(p_manifest) NOT BETWEEN 2 AND 1048576 OR p_result IS NULL
 OR p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 OR p_variant IS NULL OR p_variant NOT IN('preview1600','preview2560') THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v29_multi_preview_task_id IS NULL OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.v29_multi_preview_source(p_tenant,r.v29_multi_preview_task_id,r.config_snapshot->>'operation');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=s.project_id FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=s.run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND (id=r.v29_multi_preview_task_id OR run_id=p_run) FOR UPDATE;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND (a.id=s.root_id OR EXISTS(
 SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND
 (t.params_artifact_id=a.id OR t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)) OR a.created_by_task_id=t.id))) FOR SHARE;
 PERFORM u.id FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run FOR SHARE OF u;
 FOR o IN SELECT * FROM photo.v29_multi_preview_result(p_tenant,p_run,p_variant) LOOP
  IF o.result_id<>p_result OR o.attempt<>p_attempt OR o.fence<>p_fence THEN RETURN false;END IF;
  count_rows:=count_rows+1;
  SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=o.task_id AND purpose='task_params' FOR UPDATE;
  IF NOT FOUND THEN RETURN false;END IF;
  params:=o.params::jsonb;
  objects:=objects||jsonb_build_array(jsonb_build_object('role',o.output_role,'artifact_id',o.artifact_id,'sha256',o.sha256,'bucket',o.bucket,'key',o.object_key,'version',o.object_version,'bytes',o.bytes,'media_type',o.media_type));
  IF o.output_role='derivative_manifest' THEN manifest_id:=o.artifact_id;manifest_sha:=o.sha256;
   IF o.bytes<>octet_length(p_manifest) THEN RETURN false;END IF;
  ELSE derivative_id:=o.artifact_id;derivative_sha:=o.sha256;mime:=o.media_type;END IF;
 END LOOP;
 IF count_rows<>2 OR manifest_sha IS DISTINCT FROM encode(sha256(convert_to(p_manifest,'UTF8')),'hex') THEN RETURN false;END IF;
 m:=p_manifest::jsonb;
 IF jsonb_typeof(m)<>'object' OR NOT m ?& ARRAY['source','derivative','variant','profile_hash','width','height','media_type','color_profile','metadata_policy','alpha']
 OR (m-'source'-'derivative'-'variant'-'profile_hash'-'width'-'height'-'media_type'-'color_profile'-'metadata_policy'-'alpha')<>'{}'::jsonb
 OR m->'source' IS DISTINCT FROM params->'image' OR m->'derivative' IS DISTINCT FROM jsonb_build_object('artifact_id',derivative_id,'sha256',derivative_sha)
 OR m->>'variant' IS DISTINCT FROM p_variant OR m->'profile_hash' IS DISTINCT FROM params->'profile_hash'
 OR m->>'media_type' IS DISTINCT FROM mime OR m->>'color_profile' IS DISTINCT FROM 'srgb' OR m->>'metadata_policy' IS DISTINCT FROM 'stripped'
 OR m->'alpha' IS DISTINCT FROM to_jsonb(mime='image/webp')
 OR jsonb_typeof(m->'width') IS DISTINCT FROM 'number' OR jsonb_typeof(m->'height') IS DISTINCT FROM 'number'
 OR m->>'width' !~ '^[1-9][0-9]*$' OR m->>'height' !~ '^[1-9][0-9]*$'
 OR (m->>'width')::integer NOT BETWEEN 1 AND (CASE p_variant WHEN 'preview1600' THEN 1600 ELSE 2560 END)
 OR (m->>'height')::integer NOT BETWEEN 1 AND (CASE p_variant WHEN 'preview1600' THEN 1600 ELSE 2560 END) THEN RETURN false;END IF;
 -- Every object read by the encoded validator must still have that locator.
 FOR witness IN SELECT value FROM jsonb_array_elements(p_locations) LOOP
  IF jsonb_typeof(witness) IS DISTINCT FROM 'object' OR NOT witness ?& ARRAY['artifact_id','sha256','bucket','key','version','bytes','media_type','role','source']
   OR (witness-'artifact_id'-'sha256'-'bucket'-'key'-'version'-'bytes'-'media_type'-'role'-'source')<>'{}'::jsonb THEN RETURN false;END IF;
  SELECT * INTO current_artifact FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(witness->>'artifact_id')::uuid FOR SHARE;
  IF NOT FOUND OR current_artifact.sha256 IS DISTINCT FROM witness->>'sha256'
   OR current_artifact.bucket IS DISTINCT FROM witness->>'bucket'
   OR current_artifact.object_key IS DISTINCT FROM witness->>'key'
   OR current_artifact.object_version IS DISTINCT FROM witness->>'version'
   OR to_jsonb(current_artifact.bytes) IS DISTINCT FROM witness->'bytes'
   OR current_artifact.media_type IS DISTINCT FROM witness->>'media_type'
   OR NOT EXISTS(SELECT 1 FROM photo.v29_multi_preview_evidence(p_tenant,p_run,p_variant,p_result,p_attempt,p_fence,current_artifact.id) e
    WHERE e.sha256=current_artifact.sha256 AND e.role=witness->>'role' AND e.source=witness->>'source') THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(p_locations))<>4
  OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_locations) x WHERE x->>'artifact_id'=d.artifact_id::text AND x->>'source'='input' AND x->>'role'='task_params')
  OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_locations) x WHERE x->>'artifact_id'=params->'image'->>'artifact_id' AND x->>'source'='input')
  OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_locations) x WHERE x->>'artifact_id'=manifest_id::text AND x->>'source'='output')
  OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_locations) x WHERE x->>'artifact_id'=derivative_id::text AND x->>'source'='output') THEN RETURN false;END IF;
 receipt:=jsonb_build_object('result_id',p_result,'attempt',p_attempt,'fence',p_fence,'manifest_id',manifest_id,'manifest',p_manifest,'objects',objects,'locations',p_locations);
 IF d.v29_multi_preview_receipt IS NOT NULL THEN RETURN d.v29_multi_preview_receipt=receipt;END IF;
 UPDATE photo.backend_documents SET v29_multi_preview_receipt=receipt WHERE tenant_id=p_tenant AND id=d.id;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.record_v29_multi_preview_receipt(uuid,uuid,text,uuid,integer,bigint,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v29_multi_preview_receipt(uuid,uuid,text,uuid,integer,bigint,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_v29_multi_preview_receipt(uuid,uuid,text,uuid,integer,bigint,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
