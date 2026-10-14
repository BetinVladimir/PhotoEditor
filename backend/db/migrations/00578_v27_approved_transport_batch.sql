-- +goose Up
-- Private exact coverage/ready metadata gate, not child admission or proof of
-- canonical transport body contents. Caller verifies the complete frozen graph.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_transport_batch_ready(p_tenant uuid,p_parent uuid,p_nodes jsonb,p_transport jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; root jsonb; n integer; count_value integer;
BEGIN
 IF jsonb_typeof(p_nodes) IS DISTINCT FROM 'array' OR jsonb_typeof(p_transport) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 n:=jsonb_array_length(p_nodes);
 IF n NOT BETWEEN 1 AND 100 OR jsonb_array_length(p_transport)<>n
 OR (SELECT count(DISTINCT t->>'task_id') FROM jsonb_array_elements(p_transport) t)<>n
 OR (SELECT count(DISTINCT t->>'id') FROM jsonb_array_elements(p_nodes) t)<>n THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_transport) ORDER BY value->>'task_id' LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object'
  OR NOT(item ?& ARRAY['task_id','entry_count','entries_sha','manifest','chunks','direct_roots'])
  OR (item-'task_id'-'entry_count'-'entries_sha'-'manifest'-'chunks'-'direct_roots')<>'{}'::jsonb
  OR jsonb_typeof(item->'task_id') IS DISTINCT FROM 'string' OR jsonb_typeof(item->'entries_sha') IS DISTINCT FROM 'string'
  OR coalesce(item->>'entries_sha','') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(item->'entry_count') IS DISTINCT FROM 'number' OR (item->>'entry_count') !~ '^[1-9][0-9]*$'
  OR (item->>'entry_count')::numeric>127 OR jsonb_typeof(item->'direct_roots') IS DISTINCT FROM 'array'
  OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_nodes) node WHERE node->>'id'=item->>'task_id') THEN RETURN false;END IF;
  count_value:=(item->>'entry_count')::integer;
  IF jsonb_array_length(item->'direct_roots')<>count_value
  OR (SELECT count(DISTINCT r->>'artifact_id') FROM jsonb_array_elements(item->'direct_roots') r)<>count_value THEN RETURN false;END IF;
  PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant
  AND id IN(SELECT (r->>'artifact_id')::uuid FROM jsonb_array_elements(item->'direct_roots') r) ORDER BY id FOR SHARE;
  FOR root IN SELECT value FROM jsonb_array_elements(item->'direct_roots') LOOP
   IF jsonb_typeof(root) IS DISTINCT FROM 'object' OR NOT(root ?& ARRAY['artifact_id','sha256'])
   OR (root-'artifact_id'-'sha256')<>'{}'::jsonb OR jsonb_typeof(root->'artifact_id') IS DISTINCT FROM 'string'
   OR jsonb_typeof(root->'sha256') IS DISTINCT FROM 'string' OR coalesce(root->>'sha256','') !~ '^[0-9a-f]{64}$'
   OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(root->>'artifact_id')::uuid
    AND a.sha256=root->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL
    AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
   IF EXISTS(SELECT 1 FROM jsonb_array_elements((item->'chunks')||jsonb_build_array(item->'manifest')) t
    WHERE t->>'artifact_id'=root->>'artifact_id') THEN RETURN false;END IF;
  END LOOP;
  IF NOT photo.semantic_approved_transport_ready(p_tenant,p_parent,(item->>'task_id')::uuid,count_value,item->'manifest',item->'chunks') THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_approved_transport_batch_ready(uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_transport_batch_ready(uuid,uuid,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
