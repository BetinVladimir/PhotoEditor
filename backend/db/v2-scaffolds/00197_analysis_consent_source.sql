-- Read only the currently granted identity consents needed by confirmed
-- observations of the frozen analysis inventory. A later admission step must
-- pin these epochs in workflow_consent_links before publishing this body.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_analysis_consent_sources(p_tenant uuid,p_run uuid)
RETURNS jsonb LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH scope AS (
  SELECT w.project_id,r.input_snapshot_id
  FROM photo.runs r JOIN photo.v2_workflows w
   ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  JOIN photo.snapshots s ON s.tenant_id=r.tenant_id
   AND s.id=r.input_snapshot_id
  JOIN photo.tenants t ON t.id=r.tenant_id
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
   AND r.status='draft' AND NOT r.cancel_requested AND NOT r.graph_sealed
   AND w.workflow_type='session' AND w.renderer_version='v2.2'
   AND w.input_snapshot_id=s.id AND s.kind='v2' AND s.expires_at IS NULL
   AND t.status='active'
 ), matched AS (
  SELECT observation.asset_version_id,observation.face_id,
   observation.person_id,grant_row.id AS consent_id,grant_row.epoch
  FROM scope JOIN photo.snapshot_items item
   ON item.tenant_id=p_tenant AND item.snapshot_id=scope.input_snapshot_id
  JOIN photo.person_observations observation
   ON observation.tenant_id=item.tenant_id
   AND observation.project_id=scope.project_id
   AND observation.asset_version_id=item.asset_version_id
   AND observation.confirmed_by IS NOT NULL
  JOIN photo.project_persons person ON person.tenant_id=observation.tenant_id
   AND person.project_id=observation.project_id
   AND person.id=observation.person_id AND person.state='active'
  JOIN photo.consent_grants grant_row ON grant_row.tenant_id=person.tenant_id
   AND grant_row.project_id=person.project_id
   AND grant_row.person_id=person.id AND grant_row.purpose='identity_matching'
   AND grant_row.status='granted'
 ), grants AS (
  SELECT DISTINCT matched.consent_id,matched.epoch,matched.person_id
  FROM matched
 ), aggregate AS (
  SELECT (SELECT count(*) FROM grants) AS grant_count,
   (SELECT count(*) FROM matched) AS face_count,
   (SELECT coalesce(jsonb_agg(jsonb_build_object(
    'consent_id',consent_id,'epoch',epoch::text,'person_id',person_id,
    'purpose','identity_matching') ORDER BY consent_id), '[]'::jsonb)
    FROM grants) AS grant_body,
   (SELECT coalesce(jsonb_agg(jsonb_build_object(
    'asset_version_id',asset_version_id,'face_id',face_id,'person_id',person_id,
    'consent_id',consent_id,'epoch',epoch::text)
    ORDER BY asset_version_id,face_id), '[]'::jsonb)
    FROM matched) AS face_body
 )
 SELECT CASE WHEN EXISTS(SELECT 1 FROM scope)
  AND aggregate.grant_count<=5000 AND aggregate.face_count<=12800
  THEN jsonb_build_object('schema_version',1,'grants',aggregate.grant_body,
   'confirmed_faces',aggregate.face_body) ELSE NULL::jsonb END
 FROM aggregate;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_analysis_consent_sources(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_analysis_consent_sources(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_analysis_consent_sources(uuid,uuid) TO photo_scheduler;
