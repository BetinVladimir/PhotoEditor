-- +goose Up
-- Retain only the exact current published assembly closure on its parent workflow.
-- +goose StatementBegin
CREATE FUNCTION photo.retain_v30_delivery_assembly(p_expansion uuid,p_token uuid,p_artifact uuid,p_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE raw text;body jsonb;e photo.graph_expansions%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 refs jsonb;owner uuid;expected_count integer;locked_count integer;v_purpose constant text:='delivery.assembly';
BEGIN
 raw:=photo.v30_delivery_saved_assembly(p_expansion,p_token,p_artifact,p_sha);
 IF raw IS NULL THEN RETURN false;END IF;
 body:=raw::jsonb;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=e.tenant_id AND run_id=e.run_id FOR SHARE;
 IF w.id IS NULL OR w.id<>photo.v30_uuid5(e.run_id,'workflow') THEN RETURN false;END IF;
 owner:=photo.v30_uuid5(w.id,'v2.delivery.assemble:0');
 WITH all_refs AS (
  SELECT p_artifact artifact_id,p_sha sha256
  UNION ALL SELECT (value->>'artifact_id')::uuid,value->>'sha256' FROM jsonb_array_elements(body->'shards')
  UNION ALL SELECT (value->'config'->>'artifact_id')::uuid,value->'config'->>'sha256' FROM jsonb_array_elements(body->'presets')
  UNION ALL SELECT (item->'export'->>'artifact_id')::uuid,item->'export'->>'sha256'
   FROM jsonb_array_elements(body->'shards') shard
   JOIN photo.backend_documents d ON d.tenant_id=e.tenant_id AND d.run_id=e.run_id AND d.owner_id=owner
    AND d.artifact_id=(shard->>'artifact_id')::uuid AND d.sha256=shard->>'sha256' AND d.state='ready'
   CROSS JOIN LATERAL jsonb_array_elements(d.v30_delivery_document_jcs::jsonb->'items') item WHERE item->>'state'='ready'
  UNION ALL SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=e.parent_task_id
 ) SELECT jsonb_agg(DISTINCT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256)) INTO refs FROM all_refs;
 IF refs IS NULL OR EXISTS(SELECT 1 FROM jsonb_to_recordset(refs) x(artifact_id uuid,sha256 text)
  GROUP BY artifact_id HAVING count(*)<>1) THEN RETURN false;END IF;
 expected_count:=jsonb_array_length(refs);
 -- Lock all physical artifacts in deterministic order before linking or replay.
 PERFORM a.id FROM photo.artifacts a JOIN jsonb_to_recordset(refs) x(artifact_id uuid,sha256 text)
  ON a.id=x.artifact_id AND a.sha256=x.sha256 WHERE a.tenant_id=e.tenant_id AND a.state='ready'
  AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) ORDER BY a.id FOR SHARE OF a;
 GET DIAGNOSTICS locked_count=ROW_COUNT;
 IF locked_count<>expected_count THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_artifact_links l LEFT JOIN (
  SELECT x.*,(row_number() OVER(ORDER BY artifact_id)-1)::integer ordinal
  FROM jsonb_to_recordset(refs) x(artifact_id uuid,sha256 text)) q
  ON q.ordinal=l.ordinal AND q.artifact_id=l.artifact_id AND q.sha256=l.sha256
  WHERE l.tenant_id=e.tenant_id AND l.workflow_id=w.id AND l.purpose=v_purpose AND q.artifact_id IS NULL) THEN RETURN false;END IF;
 INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal)
 SELECT e.tenant_id,w.project_id,w.id,artifact_id,sha256,v_purpose,(row_number() OVER(ORDER BY artifact_id)-1)::integer
 FROM jsonb_to_recordset(refs) x(artifact_id uuid,sha256 text)
 ON CONFLICT(tenant_id,workflow_id,purpose,ordinal) DO NOTHING;
 IF (SELECT count(*) FROM photo.v2_artifact_links l WHERE l.tenant_id=e.tenant_id AND l.workflow_id=w.id AND l.purpose=v_purpose)<>expected_count
 OR NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=e.id AND expansion_token=p_token AND expansion_lease_until>clock_timestamp()) THEN
  RAISE EXCEPTION 'DELIVERY_ASSEMBLY_RETENTION_FENCE' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.retain_v30_delivery_assembly(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.retain_v30_delivery_assembly(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.retain_v30_delivery_assembly(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
