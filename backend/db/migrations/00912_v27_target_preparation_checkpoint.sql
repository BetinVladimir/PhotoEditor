-- +goose Up
-- Private immutable planning metadata on the original detector, no new task,
-- capability, receipt or tenant-purge table. All existing rows retain NULL.
ALTER TABLE photo.runs ADD COLUMN v27_target_manifest text;
ALTER TABLE photo.runs ADD COLUMN v27_target_manifest_sha text;
ALTER TABLE photo.runs ADD CONSTRAINT v27_target_manifest_pair CHECK (
 (v27_target_manifest IS NULL AND v27_target_manifest_sha IS NULL) OR
 (v27_target_manifest IS NOT NULL AND octet_length(v27_target_manifest) BETWEEN 2 AND 8388608
 AND v27_target_manifest_sha IS NOT NULL AND v27_target_manifest_sha ~ '^[0-9a-f]{64}$'
 AND v27_target_manifest_sha=encode(public.digest(convert_to(v27_target_manifest,'UTF8'),'sha256'),'hex')));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v27_target_manifest() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v27_target_manifest IS NOT NULL OR NEW.v27_target_manifest_sha IS NOT NULL THEN
   RAISE EXCEPTION 'V27_TARGET_MANIFEST_PRIVATE' USING ERRCODE='42501';
  END IF;
 ELSIF NEW.v27_target_manifest IS DISTINCT FROM OLD.v27_target_manifest OR NEW.v27_target_manifest_sha IS DISTINCT FROM OLD.v27_target_manifest_sha THEN
  IF current_user<>'photo_fn_owner' OR OLD.v27_target_manifest IS NOT NULL THEN
   RAISE EXCEPTION 'V27_TARGET_MANIFEST_IMMUTABLE' USING ERRCODE='42501';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER v27_target_manifest_private BEFORE INSERT OR UPDATE OF v27_target_manifest,v27_target_manifest_sha ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_target_manifest();
REVOKE ALL ON FUNCTION photo.guard_v27_target_manifest() FROM PUBLIC;
-- Serializer is private and used only for server-constructed manifest grammar:
-- ASCII field names, UUID/hex/operation strings, integers and booleans. It is not
-- a general JSON canonicalizer (arbitrary floating point input is refused).
CREATE FUNCTION photo.v27_target_manifest_json_text(p_value jsonb) RETURNS text LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE result text;
BEGIN
 CASE jsonb_typeof(p_value)
 WHEN 'object' THEN SELECT '{'||coalesce(string_agg(to_jsonb(key)::text||':'||photo.v27_target_manifest_json_text(value),',' ORDER BY key COLLATE "C"),'')||'}' INTO result FROM jsonb_each(p_value);
 WHEN 'array' THEN SELECT '['||coalesce(string_agg(photo.v27_target_manifest_json_text(value),',' ORDER BY ordinal),'')||']' INTO result FROM jsonb_array_elements(p_value) WITH ORDINALITY AS e(value,ordinal);
 WHEN 'string','boolean','null' THEN result:=p_value::text;
 WHEN 'number' THEN
  IF p_value::text !~ '^(0|[1-9][0-9]*)$' THEN RAISE EXCEPTION 'Invalid manifest integer';END IF;
  result:=p_value::text;
 ELSE RAISE EXCEPTION 'Invalid manifest scalar';
 END CASE;
 RETURN result;
END $$;
ALTER FUNCTION photo.v27_target_manifest_json_text(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_target_manifest_json_text(jsonb) FROM PUBLIC;
CREATE FUNCTION photo.save_v27_target_manifest(p_tenant uuid,p_detector uuid,p_evidence jsonb,p_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;native jsonb;snapshot jsonb;manifest jsonb;entry jsonb;binding jsonb;stages jsonb;entries jsonb[]:='{}';
 parent uuid;namespace uuid;stage_id uuid;ops text[]:=ARRAY['mask.person','face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose'];
 op text;requires jsonb;ordinal integer:=0;expected text;digest text;reuse boolean;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608 THEN RETURN NULL;END IF;
 -- Existing anchor locks tenant/usage/inventory/documents in admission order.
 IF photo.lock_semantic_native_admission_anchor(p_tenant,p_detector,p_evidence) IS NOT TRUE
 OR p_evidence->>'operation' IS DISTINCT FROM 'face.detect' THEN RETURN NULL;END IF;
 parent:=(p_evidence->>'parent_run_id')::uuid;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=parent FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_detector FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested
 OR r.status NOT IN('queued','running','succeeded') OR r.created_by::text IS DISTINCT FROM p_evidence->>'actor_id'
 OR r.config_snapshot IS DISTINCT FROM (p_evidence->>'config_text')::jsonb
 OR r.config_hash IS DISTINCT FROM encode(public.digest(convert_to(p_evidence->>'config_text','UTF8'),'sha256'),'hex')
 OR r.input_snapshot_id::text IS DISTINCT FROM (p_evidence->'anchor'->>'snapshot_text')::jsonb->>'snapshot_id'
 OR r.v27_initial_manual_selection IS NOT NULL THEN RETURN NULL;END IF;
 native:=r.config_snapshot->'v27_detector_prepare';
 snapshot:=(p_evidence->'anchor'->>'snapshot_text')::jsonb;
 -- Construct the complete expected DAG; caller cannot nominate IDs/dependencies.
 FOR entry IN SELECT value FROM jsonb_array_elements(snapshot->'items') LOOP
  binding:=entry->'binding';
  digest:=encode(public.digest(convert_to(photo.v27_target_manifest_json_text(binding),'UTF8'),'sha256'),'hex');
  namespace:=photo.semantic_uuid_v5(parent,'semantic.target.prepare:1:'||(native->'snapshot'->>'sha256')||':'||ordinal||':'||digest);
  reuse:=binding=native->'source';stages:='[]';
  IF NOT reuse THEN
   FOREACH op IN ARRAY ops LOOP
    stage_id:=photo.semantic_uuid_v5(namespace,'stage:'||op);
    requires:=CASE op
     WHEN 'face.landmarks' THEN jsonb_build_array(photo.semantic_uuid_v5(namespace,'stage:face.detect'))
     WHEN 'face.parse' THEN jsonb_build_array(photo.semantic_uuid_v5(namespace,'stage:face.detect'))
     WHEN 'mask.face_skin' THEN jsonb_build_array(photo.semantic_uuid_v5(namespace,'stage:face.parse'))
     WHEN 'defects.propose' THEN jsonb_build_array(photo.semantic_uuid_v5(namespace,'stage:mask.person'),photo.semantic_uuid_v5(namespace,'stage:face.landmarks'),photo.semantic_uuid_v5(namespace,'stage:mask.face_skin'))
     ELSE '[]'::jsonb END;
    stages:=stages||jsonb_build_array(jsonb_build_object('id',stage_id,'operation',op,'operation_version','1','requires',requires));
   END LOOP;
  END IF;
  entries:=array_append(entries,jsonb_build_object('ordinal',ordinal,'binding',binding,'namespace',namespace,'reuse_reference',reuse,'stages',stages));
  ordinal:=ordinal+1;
 END LOOP;
 manifest:=jsonb_build_object('schema_version',1,'tenant_id',p_tenant,'project_id',p_evidence->'project_id','actor_id',r.created_by,
  'session_id',native->'auth_session_id','parent_run_id',parent,'request_sha',native->'request_sha','intent',native->'intent','snapshot',native->'snapshot','entries',to_jsonb(entries));
 expected:=photo.v27_target_manifest_json_text(manifest);
 IF p_body IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 digest:=encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex');
 IF r.v27_target_manifest IS NOT NULL THEN
  IF r.v27_target_manifest<>p_body OR r.v27_target_manifest_sha<>digest THEN RETURN NULL;END IF;
  RETURN jsonb_build_object('body',r.v27_target_manifest,'sha',r.v27_target_manifest_sha);
 END IF;
 UPDATE photo.runs SET v27_target_manifest=p_body,v27_target_manifest_sha=digest WHERE tenant_id=p_tenant AND id=p_detector;
 RETURN jsonb_build_object('body',p_body,'sha',digest);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.save_v27_target_manifest(uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v27_target_manifest(uuid,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v27_target_manifest(uuid,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
