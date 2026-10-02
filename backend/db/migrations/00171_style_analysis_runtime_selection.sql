-- Select qualified Analyze runtimes without exposing worker registrations to photo_api.
-- +goose Up
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.style_analysis_builds,photo.style_analysis_nodes TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.select_style_analysis_runtimes(p_tenant uuid,p_geometry boolean)
RETURNS TABLE(fit_lock_id text,fit_backend text,aggregate_lock_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN;END IF;
 RETURN QUERY
 SELECT coalesce(fit.lock_id,''),coalesce(fit.backend,''),coalesce(aggregate.lock_id,'') FROM (VALUES(1)) seed(n)
 LEFT JOIN LATERAL (SELECT w.lock_id,CASE
   WHEN (w.qualified_operations->'pair.fit.color') ? 'cuda' AND w.backends ? 'cuda' AND (w.qualified_operations->'face.detect') ? 'cuda' AND (w.qualified_operations->'face.landmarks') ? 'cuda' AND (w.qualified_operations->'face.parse') ? 'cuda'
    AND (w.qualified_operations->'mask.person') ? 'cuda' AND (NOT p_geometry OR (w.qualified_operations->'pose.detect') ? 'cuda') THEN 'cuda'
   WHEN (w.qualified_operations->'pair.fit.color') ? 'mps' AND w.backends ? 'mps' AND (w.qualified_operations->'face.detect') ? 'mps' AND (w.qualified_operations->'face.landmarks') ? 'mps' AND (w.qualified_operations->'face.parse') ? 'mps'
    AND (w.qualified_operations->'mask.person') ? 'mps' AND (NOT p_geometry OR (w.qualified_operations->'pose.detect') ? 'mps') THEN 'mps' ELSE 'cpu' END backend
  FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant AND w.enabled AND w.last_seen>clock_timestamp()-interval '30 seconds'
   AND w.resource_classes ? 'gpu' AND (((w.qualified_operations->'pair.fit.color') ? 'cuda' AND w.backends ? 'cuda' AND (w.qualified_operations->'face.detect') ? 'cuda' AND (w.qualified_operations->'face.landmarks') ? 'cuda' AND (w.qualified_operations->'face.parse') ? 'cuda' AND (w.qualified_operations->'mask.person') ? 'cuda' AND (NOT p_geometry OR (w.qualified_operations->'pose.detect') ? 'cuda'))
    OR ((w.qualified_operations->'pair.fit.color') ? 'mps' AND w.backends ? 'mps' AND (w.qualified_operations->'face.detect') ? 'mps' AND (w.qualified_operations->'face.landmarks') ? 'mps' AND (w.qualified_operations->'face.parse') ? 'mps' AND (w.qualified_operations->'mask.person') ? 'mps' AND (NOT p_geometry OR (w.qualified_operations->'pose.detect') ? 'mps'))
    OR ((w.qualified_operations->'pair.fit.color') ? 'cpu' AND w.backends ? 'cpu' AND (w.qualified_operations->'face.detect') ? 'cpu' AND (w.qualified_operations->'face.landmarks') ? 'cpu' AND (w.qualified_operations->'face.parse') ? 'cpu' AND (w.qualified_operations->'mask.person') ? 'cpu' AND (NOT p_geometry OR (w.qualified_operations->'pose.detect') ? 'cpu')))
   AND w.lock_id ~ '^[0-9a-f]{64}$' ORDER BY CASE WHEN (w.qualified_operations->'pair.fit.color') ? 'cuda' AND w.backends ? 'cuda' AND (w.qualified_operations->'face.detect') ? 'cuda' AND (w.qualified_operations->'face.landmarks') ? 'cuda' AND (w.qualified_operations->'face.parse') ? 'cuda' THEN 0 WHEN (w.qualified_operations->'pair.fit.color') ? 'mps' AND w.backends ? 'mps' AND (w.qualified_operations->'face.detect') ? 'mps' AND (w.qualified_operations->'face.landmarks') ? 'mps' AND (w.qualified_operations->'face.parse') ? 'mps' THEN 1 ELSE 2 END,w.worker_id LIMIT 1) fit ON true
 LEFT JOIN LATERAL (SELECT w.lock_id FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant AND w.enabled AND w.last_seen>clock_timestamp()-interval '30 seconds'
   AND w.resource_classes ? 'cpu' AND (w.qualified_operations->'style.aggregate') ? 'cpu' AND (w.qualified_operations->'mask.face_skin') ? 'cpu' AND (w.qualified_operations->'pair.fit.beauty') ? 'cpu' AND (w.qualified_operations->'pair.fit.defects') ? 'cpu' AND (w.qualified_operations->'style.validate') ? 'cpu' AND (w.qualified_operations->'style.package') ? 'cpu' AND w.backends ? 'cpu'
   AND (NOT p_geometry OR ((w.qualified_operations->'pair.fit.body') ? 'cpu' AND (w.qualified_operations->'geometry.body') ? 'cpu'))
   AND w.lock_id ~ '^[0-9a-f]{64}$' ORDER BY w.worker_id LIMIT 1) aggregate ON true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.select_style_analysis_runtimes(uuid,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.select_style_analysis_runtimes(uuid,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.select_style_analysis_runtimes(uuid,boolean) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.create_style_analysis_build(p_tenant uuid,p_run uuid,p_style uuid,p_pairs int)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n int;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_pairs NOT BETWEEN 1 AND 32 THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.runs r JOIN photo.styles s ON s.tenant_id=r.tenant_id AND s.id=p_style
   WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='analyze' AND r.status='queued' AND NOT r.graph_sealed
    AND r.config_snapshot->>'style_id'=p_style::text AND r.input_snapshot_id IS NOT NULL) THEN RETURN false;END IF;
 INSERT INTO photo.style_analysis_builds(tenant_id,run_id,style_id,phase,pair_count) VALUES(p_tenant,p_run,p_style,'registration',p_pairs)
 ON CONFLICT(tenant_id,run_id) DO NOTHING;
 GET DIAGNOSTICS n=ROW_COUNT;
 -- Deferred FK triggers otherwise execute at the photo_api transaction commit,
 -- outside this trusted function and correctly fail against the private table.
 SET CONSTRAINTS ALL IMMEDIATE;
 RETURN n=1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_style_analysis_build(uuid,uuid,uuid,int) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_style_analysis_build(uuid,uuid,uuid,int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_style_analysis_build(uuid,uuid,uuid,int) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
