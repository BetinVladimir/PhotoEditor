-- The trusted compiler reads only the frozen run snapshot. Object locations
-- are never exposed to API or worker roles by these functions.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_analysis_image_sources(p_tenant uuid,p_run uuid)
RETURNS TABLE(ordinal integer,asset_version_id uuid,artifact_id uuid,sha256 text,
 bytes bigint,bucket text,object_key text,object_version text,media_type text,
 captured_at text,camera_hash text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT i.ordinal,i.asset_version_id,a.id,a.sha256,a.bytes,a.bucket,
  a.object_key,a.object_version,a.media_type,i.payload->>'captured_at',
  i.payload->>'camera_hash'
 FROM photo.runs r JOIN photo.v2_workflows w
  ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
  AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
  AND a.sha256=i.sha256
 JOIN photo.tenants t ON t.id=r.tenant_id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
  AND r.status='draft' AND NOT r.cancel_requested AND NOT r.graph_sealed
  AND w.workflow_type='session' AND w.renderer_version='v2.2'
  AND w.input_snapshot_id=s.id AND s.kind='v2' AND s.expires_at IS NULL
  AND s.principal_type='user' AND s.principal_id=r.created_by
  AND s.digest=r.config_snapshot->'v22_session_plan'->>'snapshot_digest'
  AND s.item_count=(r.config_snapshot->'v22_session_plan'->>'item_count')::integer
  AND t.status='active' AND i.ordinal BETWEEN 0 AND s.item_count-1
  AND a.state='ready' AND a.output_role='working_image'
  AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
  AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 ORDER BY i.ordinal;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_analysis_image_sources(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_analysis_image_sources(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_analysis_image_sources(uuid,uuid) TO photo_scheduler;

-- A selected recipe is the exact immutable document pinned in the pending
-- recovery packet, even if the current draft stream advances before retry.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_analysis_recipe_sources(p_tenant uuid,p_run uuid)
RETURNS TABLE(ordinal integer,asset_version_id uuid,artifact_id uuid,sha256 text,
 bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT i.ordinal,i.asset_version_id,d.artifact_id,d.sha256,
  a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.runs r JOIN photo.v2_workflows w
  ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.idempotency_records idem ON idem.tenant_id=r.tenant_id
  AND idem.resource_id=r.id AND idem.principal_type='user'
  AND idem.principal_id=r.created_by AND idem.route='V2.Admit'
  AND idem.response_status IS NULL
 JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id
 JOIN photo.v2_document_streams stream ON stream.tenant_id=r.tenant_id
  AND stream.project_id=w.project_id AND stream.asset_version_id=i.asset_version_id
  AND stream.stream_type='draft'
 JOIN photo.v2_documents d ON d.tenant_id=stream.tenant_id
  AND d.stream_id=stream.id
  AND d.project_id=stream.project_id AND d.document_type='EditingRecipeV3'
  AND d.artifact_id::text=idem.response_body->'snapshot'->'items'->(i.ordinal::integer)->'recipe'->>'artifact_id'
  AND d.sha256=idem.response_body->'snapshot'->'items'->(i.ordinal::integer)->'recipe'->>'sha256'
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  AND a.sha256=d.sha256
 JOIN photo.tenants t ON t.id=r.tenant_id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
  AND r.status='draft' AND NOT r.cancel_requested AND NOT r.graph_sealed
  AND w.workflow_type='session' AND w.renderer_version='v2.2'
  AND w.input_snapshot_id=s.id
  AND s.kind='v2' AND s.expires_at IS NULL
  AND s.digest=r.config_snapshot->'v22_session_plan'->>'snapshot_digest'
  AND t.status='active' AND a.state='ready' AND a.output_role='document'
  AND a.media_type='application/json' AND a.object_version IS NOT NULL
  AND a.object_version NOT IN ('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 ORDER BY i.ordinal;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_analysis_recipe_sources(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_analysis_recipe_sources(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_analysis_recipe_sources(uuid,uuid) TO photo_scheduler;

-- Resolve the complete selected inventory before freeze, under the same
-- authorization and revision preflight used by freeze itself. The compiler
-- hashes these refs into InputSnapshot; freeze then copies exactly these
-- snapshot_items and repeats preflight in its own transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_analysis_selection_sources(p_tenant uuid,p_session uuid,
 p_expected bigint,p_selection uuid,p_manual jsonb,p_actor uuid,p_auth_session uuid)
RETURNS TABLE(ordinal integer,asset_version_id uuid,artifact_id uuid,sha256 text,
 bytes bigint,bucket text,object_key text,object_version text,media_type text,
 captured_at text,camera_hash text,recipe_artifact_id uuid,recipe_sha256 text,
 recipe_bytes bigint,recipe_bucket text,recipe_key text,recipe_version text,
 recipe_media_type text,
 selection_item_count bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH authorized AS (
  SELECT photo.preflight_v22_analysis(p_tenant,p_session,p_expected,p_selection,
   p_manual,p_actor,p_auth_session) AS context
 )
 SELECT i.ordinal,i.asset_version_id,a.id,a.sha256,a.bytes,a.bucket,
  a.object_key,a.object_version,a.media_type,i.payload->>'captured_at',
  i.payload->>'camera_hash',d.artifact_id,d.sha256,recipe.bytes,
  recipe.bucket,recipe.object_key,recipe.object_version,recipe.media_type,
  s.item_count
 FROM authorized gate JOIN photo.snapshots s
  ON s.tenant_id=p_tenant AND s.id=p_selection
 JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id
  AND v.id=i.asset_version_id AND v.master_artifact_id=i.artifact_id
  AND v.content_hash=i.sha256
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
  AND a.sha256=i.sha256
 JOIN photo.shoot_sessions session ON session.tenant_id=p_tenant
  AND session.id=p_session AND session.collection_id=s.collection_id
 JOIN photo.v2_document_streams stream ON stream.tenant_id=p_tenant
  AND stream.project_id=session.project_id AND stream.asset_version_id=i.asset_version_id
  AND stream.stream_type='draft'
 JOIN photo.v2_documents d ON d.tenant_id=stream.tenant_id
  AND d.id=stream.current_document_id AND d.stream_id=stream.id
  AND d.project_id=stream.project_id AND d.document_type='EditingRecipeV3'
 JOIN photo.artifacts recipe ON recipe.tenant_id=d.tenant_id
  AND recipe.id=d.artifact_id AND recipe.sha256=d.sha256
 WHERE gate.context->>'selection_snapshot_id'=p_selection::text
  AND a.state='ready' AND a.output_role='working_image'
  AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
  AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND recipe.state='ready' AND recipe.output_role='document'
  AND recipe.media_type='application/json'
  AND recipe.object_version IS NOT NULL AND recipe.object_version NOT IN ('','null')
  AND (recipe.expires_at IS NULL OR recipe.expires_at>clock_timestamp())
 ORDER BY i.ordinal;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_analysis_selection_sources(uuid,uuid,bigint,uuid,jsonb,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_analysis_selection_sources(uuid,uuid,bigint,uuid,jsonb,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_analysis_selection_sources(uuid,uuid,bigint,uuid,jsonb,uuid,uuid)
 TO photo_scheduler;

-- Child ref must be a ready output of the exact same canonical producer
-- attempt as the WorkingImage declared by the frozen snapshot item.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_analysis_image_child(p_tenant uuid,p_run uuid,
 p_parent uuid,p_parent_sha text,p_child uuid,p_child_sha text,p_role text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT child.id,child.sha256,child.bytes,child.bucket,child.object_key,
  child.object_version,child.media_type
 FROM photo.runs r JOIN photo.v2_workflows w
  ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id
  AND i.snapshot_id=r.input_snapshot_id AND i.artifact_id=p_parent
  AND i.sha256=p_parent_sha
 JOIN photo.artifacts parent ON parent.tenant_id=i.tenant_id
  AND parent.id=i.artifact_id AND parent.sha256=i.sha256
 JOIN photo.artifacts child ON child.tenant_id=parent.tenant_id
  AND child.id=p_child AND child.sha256=p_child_sha
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
  AND r.status='draft' AND NOT r.cancel_requested AND NOT r.graph_sealed
  AND w.workflow_type='session' AND w.renderer_version='v2.2'
  AND parent.state='ready' AND parent.output_role='working_image'
  AND parent.created_by_task_id IS NOT NULL
  AND parent.produced_attempt IS NOT NULL AND parent.produced_fence IS NOT NULL
  AND child.created_by_task_id=parent.created_by_task_id
  AND child.produced_attempt=parent.produced_attempt
  AND child.produced_fence=parent.produced_fence
  AND p_role IN ('working_rgb','alpha') AND child.output_role=p_role
  AND child.media_type='application/octet-stream' AND child.bytes>0
  AND child.state='ready' AND child.object_version IS NOT NULL
  AND child.object_version NOT IN ('','null')
  AND (child.expires_at IS NULL OR child.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_analysis_image_child(uuid,uuid,uuid,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_analysis_image_child(uuid,uuid,uuid,text,uuid,text,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_analysis_image_child(uuid,uuid,uuid,text,uuid,text,text)
 TO photo_scheduler;
