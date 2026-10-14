-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.v30_metadata_document_scope(uuid,uuid,uuid,text,text)'::regprocedure) IS DISTINCT FROM '7a8287dc72eb027b63e9d057c8d718ad' THEN RAISE EXCEPTION 'METADATA_DOCUMENT_SCOPE_DRIFT';END IF;END $$;
CREATE OR REPLACE FUNCTION photo.v30_metadata_document_scope(p_tenant uuid,p_expansion uuid,p_token uuid,p_purpose text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE proof jsonb;body text;pin text;ref jsonb;rid uuid;entries text;chunk_ref jsonb;
BEGIN
 IF p_purpose IS NULL OR p_purpose NOT IN('task_params','v30_metadata_projection','v2_input_chunk_0','v2_input_manifest') OR p_body IS NULL THEN RETURN NULL;END IF;
 SELECT proof_jcs,proof_sha INTO body,pin FROM photo.read_v30_delivery_metadata_proof(p_tenant,p_expansion,p_token);
 IF body IS NULL THEN RETURN NULL;END IF;
 proof:=body::jsonb;
 IF p_purpose='task_params' THEN ref:=proof->'params';body:=proof->>'params_jcs';
 ELSIF p_purpose='v30_metadata_projection' THEN ref:=proof->'projection';body:=proof->>'projection_jcs';
 ELSE
  SELECT '['||string_agg(format('{"contract":"%s","ref":{"artifact_id":"%s","sha256":"%s"}}',contract,
   value->>'artifact_id',value->>'sha256'),',' ORDER BY (value->>'artifact_id')::uuid)||']' INTO entries
  FROM (VALUES(proof->'params','Params'),(proof->'encoded','EncodedImage'),
   ((proof->>'encoded_jcs')::jsonb->'artifact','EncodedRaster'),(proof->'projection','MetadataDocument')) refs(value,contract);
  body:='{"entries":'||entries||',"schema_version":1}';
  chunk_ref:=jsonb_build_object('artifact_id',photo.v30_uuid5((proof->>'task_id')::uuid,'delivery:metadata:transport:v2_input_chunk_0:1'),
    'sha256',encode(sha256(convert_to(body,'UTF8')),'hex'));
  IF p_purpose='v2_input_manifest' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
    WHERE b.tenant_id=p_tenant AND b.owner_id=(proof->>'task_id')::uuid AND b.purpose='v2_input_chunk_0' AND b.state='ready'
    AND b.artifact_id=(chunk_ref->>'artifact_id')::uuid AND b.sha256=chunk_ref->>'sha256'
    AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version=b.object_version
    AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
   body:=format('{"chunks":[{"artifact_id":"%s","sha256":"%s"}],"entries_sha":"%s","entry_count":4,"schema_version":1,"task_id":"%s"}',
    chunk_ref->>'artifact_id',chunk_ref->>'sha256',encode(sha256(convert_to(entries,'UTF8')),'hex'),proof->>'task_id');
  END IF;
  ref:=jsonb_build_object('artifact_id',photo.v30_uuid5((proof->>'task_id')::uuid,'delivery:metadata:transport:'||p_purpose||':1'),
   'sha256',encode(sha256(convert_to(body,'UTF8')),'hex'));
 END IF;
 IF body IS DISTINCT FROM p_body OR octet_length(body) NOT BETWEEN 2 AND 1048576
 OR (p_purpose='task_params' AND octet_length(body)>65536) THEN RETURN NULL;END IF;
 SELECT run_id INTO rid FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion;
 RETURN jsonb_build_object('run',rid,'owner',proof->>'task_id','artifact',ref->>'artifact_id','sha',ref->>'sha256','bytes',octet_length(body));
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
