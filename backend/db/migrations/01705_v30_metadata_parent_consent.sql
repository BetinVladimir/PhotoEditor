-- +goose Up
-- Parent frozen consent is authoritative for every metadata child.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_policy_approval_valid(p_policy jsonb,p_mode text,p_approved text[]) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE fields jsonb;names text[];
BEGIN
 IF jsonb_typeof(p_policy) IS DISTINCT FROM 'object' OR p_mode IS NULL
 OR p_mode NOT IN('strip','copyright','approved_exif')
 OR p_policy->>'metadata_policy' IS DISTINCT FROM p_mode OR p_approved IS NULL
 OR cardinality(p_approved)>7 OR array_position(p_approved,NULL) IS NOT NULL
 OR (p_mode<>'approved_exif' AND cardinality(p_approved)>0)
 OR EXISTS(SELECT 1 FROM unnest(p_approved) x WHERE x NOT IN('author','copyright','camera_make','camera_model','lens','datetime_original','gps'))
 OR (SELECT count(DISTINCT x) FROM unnest(p_approved) x)<>cardinality(p_approved) THEN RETURN false;END IF;
 fields:=coalesce(p_policy->'approved_fields','[]'::jsonb);
 IF jsonb_typeof(fields) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 SELECT coalesce(array_agg(x ORDER BY x COLLATE "C"),ARRAY[]::text[]) INTO names FROM unnest(p_approved) x;
 RETURN fields=to_jsonb(names);
END $$;
ALTER FUNCTION photo.v30_metadata_policy_approval_valid(jsonb,text,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_policy_approval_valid(jsonb,text,text[]) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.v30_metadata_child_source_ready(uuid,uuid,jsonb,jsonb,text[])'::regprocedure) IS DISTINCT FROM '8dba47e5b8741c9ff7863ee911e8adaa' THEN RAISE EXCEPTION 'METADATA_PARENT_CONSENT_DEFINITION_DRIFT';END IF;END $$;
CREATE OR REPLACE FUNCTION photo.v30_metadata_child_source_ready(p_tenant uuid,p_child uuid,p_config jsonb,p_proof jsonb,p_approved text[]) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;parent uuid;publication_sha text;project uuid;names text[];frozen jsonb;
BEGIN
 IF jsonb_typeof(p_config) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(p_config))<>8
 OR p_config->>'queue_class' IS DISTINCT FROM 'batch'
 OR p_config->'v30_metadata_source' IS DISTINCT FROM p_proof->'metadata_source'
 OR p_config->>'v30_metadata_frozen_sha256' IS NULL
 OR p_config->>'v30_metadata_frozen_sha256' IS DISTINCT FROM p_proof->>'frozen_sha256'
 OR coalesce(p_config->>'v30_metadata_publication_sha256','') !~ '^[0-9a-f]{64}$'
 OR p_approved IS NULL THEN RETURN false;END IF;
 SELECT coalesce(array_agg(x ORDER BY x COLLATE "C"),ARRAY[]::text[]) INTO names FROM unnest(p_approved) x;
 IF p_config->'v30_metadata_approved_fields' IS DISTINCT FROM to_jsonb(names) THEN RETURN false;END IF;
 request:=p_config->'v30_delivery_export';parent:=(request->>'parent_run_id')::uuid;project:=(p_config->>'project_id')::uuid;
 IF request->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR request->>'child_run_id' IS DISTINCT FROM p_child::text
 OR request->'binding' IS DISTINCT FROM p_proof->'binding'
 OR ((request->>'config_jcs')::jsonb)->>'metadata_policy' IS DISTINCT FROM p_proof->>'policy'
 OR (p_config->>'export_id')::uuid IS NULL OR (p_config->>'export_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR parent IS NULL OR project IS NULL THEN RETURN false;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
 FROM photo.backend_documents b JOIN photo.tasks prepare
 ON prepare.tenant_id=b.tenant_id AND prepare.id=b.owner_id AND prepare.run_id=parent
 WHERE b.tenant_id=p_tenant AND b.run_id=parent AND b.purpose='task_params' AND b.state='ready'
 AND prepare.operation='v2.delivery.prepare' AND b.artifact_id=prepare.params_artifact_id;
 IF photo.v30_metadata_policy_approval_valid(frozen->'policy',p_proof->>'policy',p_approved) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF photo.v30_metadata_published_source_ready(p_tenant,parent,p_proof,p_approved) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT d.v30_metadata_source_sha INTO publication_sha FROM photo.backend_documents d
 WHERE d.tenant_id=p_tenant AND d.run_id=parent AND d.purpose='v30_metadata_source' AND d.state='ready'
 AND d.artifact_id=(p_proof->'metadata_source'->>'artifact_id')::uuid
 AND (d.v30_metadata_source_jcs::jsonb)->'scope'->>'project_id'=project::text;
 RETURN publication_sha IS NOT NULL AND publication_sha=p_config->>'v30_metadata_publication_sha256';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_child_source_ready(uuid,uuid,jsonb,jsonb,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_child_source_ready(uuid,uuid,jsonb,jsonb,text[]) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
