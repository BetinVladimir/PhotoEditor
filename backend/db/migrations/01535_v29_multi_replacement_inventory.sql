-- +goose Up
-- Pure inventory mirror is private; authority comes from the scoped lock gate.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_replacement_inventory_valid(p_draft text)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE packet jsonb;descriptor jsonb;params jsonb;pin jsonb;roots jsonb;physical jsonb:='[]'::jsonb;
 source jsonb;frame jsonb;child jsonb;item jsonb;ref jsonb;entries jsonb;encoded text;input_jcs text;i integer;n integer;
BEGIN
 IF p_draft IS NULL OR octet_length(p_draft) NOT BETWEEN 2 AND 41943040 THEN RETURN false;END IF;
 packet:=p_draft::jsonb;descriptor:=packet->'draft';params:=(descriptor->>'params_jcs')::jsonb;
 pin:=descriptor->'execution_profile'->'multi_qualification';
 IF descriptor->>'operation' IS NULL OR descriptor->>'operation' NOT IN('optical.panorama','optical.focus_stack')
 OR jsonb_typeof(params->'frames') IS DISTINCT FROM 'array'
 OR jsonb_typeof(descriptor->'sources_jcs') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 n:=jsonb_array_length(params->'frames');
 IF n NOT BETWEEN 2 AND 30 OR jsonb_array_length(descriptor->'sources_jcs')<>n THEN RETURN false;END IF;
 roots:=jsonb_build_array(
 jsonb_build_object('contract','Params','ref',jsonb_build_object('artifact_id',descriptor->>'params_document_id','sha256',descriptor->>'params_sha')),
 jsonb_build_object('contract','Calibration','ref',pin->'calibration'),jsonb_build_object('contract','Report','ref',pin->'report'));
 FOR i IN 0..n-1 LOOP
 frame:=params->'frames'->i;source:=(descriptor->'sources_jcs'->>i)::jsonb;
 roots:=roots||jsonb_build_array(jsonb_build_object('contract','WorkingImage','ref',frame->'source'));
 physical:=physical||jsonb_build_array(jsonb_build_object('contract','NpyArray','ref',source->'pixels'));
 IF source->'alpha' IS NOT NULL AND source->'alpha'<>'null'::jsonb THEN
 IF source->'alpha'->>'artifact_id'=source->'pixels'->>'artifact_id' THEN RETURN false;END IF;
 physical:=physical||jsonb_build_array(jsonb_build_object('contract','NpyArray','ref',source->'alpha'));END IF;
 IF jsonb_typeof(source->'transform_chain'->'steps') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(source->'transform_chain'->'steps')>128 THEN RETURN false;END IF;
 FOR child IN SELECT value FROM jsonb_array_elements(source->'transform_chain'->'steps') LOOP
 IF child->'inverse_field' IS NOT NULL AND child->'inverse_field'<>'null'::jsonb THEN
 physical:=physical||jsonb_build_array(jsonb_build_object('contract','NpyArray','ref',child->'inverse_field'));END IF;
 END LOOP;
 END LOOP;
 FOR item IN SELECT value FROM jsonb_array_elements(roots||physical) LOOP
 ref:=item->'ref';
 IF jsonb_typeof(ref) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(ref) k) IS DISTINCT FROM ARRAY['artifact_id','sha256']::text[]
 OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
 OR (ref->>'artifact_id')::uuid IS NULL OR (ref->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR (ref->>'artifact_id')::uuid::text IS DISTINCT FROM ref->>'artifact_id'
 OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string' OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 -- Identical repeated frame/physical references are deduplicated; cross-contract
 -- aliases and same-ID/different-SHA references are never accepted.
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(roots||physical) a JOIN jsonb_array_elements(roots||physical) b
 ON a->'ref'->>'artifact_id'=b->'ref'->>'artifact_id' AND a.value IS DISTINCT FROM b.value) THEN RETURN false;END IF;
 SELECT jsonb_agg(value ORDER BY (value->'ref'->>'artifact_id') COLLATE "C") INTO entries
 FROM (SELECT DISTINCT value FROM jsonb_array_elements(roots||physical)) e;
 IF jsonb_array_length(entries)>4096 THEN RETURN false;END IF;
 SELECT string_agg('{"contract":'||to_jsonb(value->>'contract')::text||',"ref":{"artifact_id":'||to_jsonb(value->'ref'->>'artifact_id')::text||',"sha256":'||to_jsonb(value->'ref'->>'sha256')::text||'}}',',' ORDER BY (value->'ref'->>'artifact_id') COLLATE "C") INTO encoded FROM jsonb_array_elements(entries);
 input_jcs:='{"entries":['||encoded||'],"operation":'||to_jsonb(descriptor->>'operation')::text||',"task":'||to_jsonb(descriptor->>'task_id')::text||',"version":"1"}';
 RETURN encode(sha256(convert_to(input_jcs,'UTF8')),'hex') IS NOT DISTINCT FROM packet->>'input_digest';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_multi_replacement_inventory_valid(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_replacement_inventory_valid(text) FROM PUBLIC;
CREATE FUNCTION photo.lock_v29_multi_replacement_inventory(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_draft text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.lock_v29_multi_replacement_identity(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan,p_draft) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.v29_multi_replacement_inventory_valid(p_draft);
END $$;
ALTER FUNCTION photo.lock_v29_multi_replacement_inventory(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_multi_replacement_inventory(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v29_multi_replacement_inventory(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
