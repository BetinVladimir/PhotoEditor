-- +goose Up
-- Internal guard for future fenced reserve/complete functions. No caller gains
-- publication privileges from this migration alone.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_document_scope(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
 SELECT 1 FROM photo.runs r
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(r.config_snapshot->'v25_lens'->'nodes')='array'
 THEN r.config_snapshot->'v25_lens'->'nodes' ELSE '[]'::jsonb END) node
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2' AND r.status='draft'
 AND NOT r.graph_sealed AND NOT r.cancel_requested AND w.renderer_version='v2.2'
 AND q.state='reserved' AND q.expires_at>clock_timestamp()
 AND node->>'task_id'=p_owner::text
 AND jsonb_array_length(CASE WHEN jsonb_typeof(r.config_snapshot->'v25_lens'->'nodes')='array'
 THEN r.config_snapshot->'v25_lens'->'nodes' ELSE '[]'::jsonb END) BETWEEN 1 AND 5000
 AND p_sha ~ '^[0-9a-f]{64}$'
 AND CASE p_purpose
 WHEN 'v25_lens_params' THEN p_sha=node->>'params_sha'
 AND p_sha=encode(sha256(convert_to(node->>'params_jcs','UTF8')),'hex')
 AND (node->>'params_jcs')::jsonb=node->'params'
 WHEN 'v25_lens_original_binding' THEN p_sha=node->>'original_sha'
 AND p_sha=encode(sha256(convert_to(node->>'original_jcs','UTF8')),'hex')
 AND (node->>'original_jcs')::jsonb=node->'original'
 ELSE false END
 AND photo.v2_editor_authorized(p_tenant,w.project_id,r.created_by,
 CASE WHEN r.config_snapshot->'v25_lens'->>'auth_session_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 THEN (r.config_snapshot->'v25_lens'->>'auth_session_id')::uuid ELSE NULL END));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_document_scope(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_document_scope(uuid,uuid,uuid,text,text) FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
