-- +goose Up
-- Caller locks authorized inventory first. Immutable snapshot body is compiler-verified.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_proposal_snapshot(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_run uuid,p_snapshot uuid,p_artifact uuid,p_sha text,p_body text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb; found_id uuid; item jsonb; matched integer:=0; expected integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_run IS NULL OR p_run='00000000-0000-0000-0000-000000000000'::uuid
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_sha
 OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN false; END IF;
 body:=p_body::jsonb;
 IF body->>'schema_version' IS DISTINCT FROM '1' OR body->>'snapshot_id' IS DISTINCT FROM p_snapshot::text
 OR jsonb_typeof(body->'items') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 expected:=jsonb_array_length(body->'items');
 IF expected NOT BETWEEN 1 AND 5000 THEN RETURN false;END IF;
 SELECT a.id INTO found_id FROM photo.v2_draft_uploads u
 JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id
 WHERE u.tenant_id=p_tenant AND u.project_id=p_project
 AND u.document_id=photo.semantic_uuid_v5(p_run,'semantic.input_snapshot')
 AND u.asset_version_id=(body->'items'->0->'binding'->>'asset_version_id')::uuid
 AND u.artifact_id=p_artifact AND u.sha256=p_sha AND u.bytes=octet_length(p_body)
 AND u.upload_contract='Document' AND u.state='ready' AND u.completed_at IS NOT NULL
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='document'
 AND a.media_type='application/json' AND a.sha256=u.sha256 AND a.bytes=u.bytes
 AND a.bucket=u.bucket AND a.object_version=u.object_version
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.object_key='tenants/'||p_tenant||'/v2-drafts/'||p_artifact
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 FOR SHARE OF u,a;
 IF found_id IS NULL THEN RETURN false;END IF;
 -- Lock stream heads deterministically; document rows are immutable. Retain
 -- recipe artifacts against expiry/state changes until caller commit.
 FOR item IN
  SELECT pins.value FROM jsonb_array_elements(body->'items') pins
  JOIN photo.snapshot_items i ON i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot
   AND i.ordinal=(pins.value->>'ordinal')::integer
   AND i.asset_version_id=(pins.value->'binding'->>'asset_version_id')::uuid
   AND i.artifact_id=(pins.value->'binding'->'source'->>'artifact_id')::uuid
   AND i.sha256=pins.value->'binding'->'source'->>'sha256'
   AND i.payload->>'canvas_hash'=pins.value->'binding'->>'canvas_hash'
  JOIN photo.v2_document_streams s ON s.tenant_id=i.tenant_id AND s.project_id=p_project
   AND s.asset_version_id=i.asset_version_id AND s.stream_type='draft'
  JOIN photo.v2_documents d ON d.tenant_id=s.tenant_id AND d.id=s.current_document_id
   AND d.stream_id=s.id AND d.project_id=s.project_id AND d.document_type='EditingRecipeV3'
   AND d.artifact_id=(pins.value->'recipe'->>'artifact_id')::uuid AND d.sha256=pins.value->'recipe'->>'sha256'
  JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
  WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='document'
   AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  ORDER BY s.id FOR SHARE OF s,a
 LOOP matched:=matched+1; END LOOP;
 RETURN matched=expected;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.lock_semantic_proposal_snapshot(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_snapshot(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_snapshot(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
