-- Read-only preflight for the trusted V22 compiler. The draft-shell/admission
-- transaction must repeat revision, selection and qualification checks before
-- it can queue a workflow; this function alone never creates a run.
-- +goose StatementBegin
CREATE FUNCTION photo.preflight_v22_analysis(
 p_tenant uuid,p_session uuid,p_expected bigint,p_selection uuid,
 p_manual jsonb,p_actor uuid,p_auth_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE session_row photo.shoot_sessions%ROWTYPE;
 snapshot_row photo.snapshots%ROWTYPE;
 ref jsonb; prev_members jsonb; context_refs jsonb:='[]'::jsonb;
 seen_groups uuid[]:=ARRAY[]::uuid[]; chosen uuid; previous uuid; chosen_group uuid;
 item_count bigint;
BEGIN
 IF p_tenant IS NULL OR p_session IS NULL OR p_selection IS NULL OR
  p_actor IS NULL OR p_auth_session IS NULL OR p_expected IS NULL OR
  p_expected<1 OR jsonb_typeof(p_manual) IS DISTINCT FROM 'array' THEN
  RAISE EXCEPTION 'V22_ANALYSIS_INVALID' USING ERRCODE='22023';
 END IF;
 IF jsonb_array_length(p_manual)>5000 THEN
  RAISE EXCEPTION 'V22_ANALYSIS_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO session_row FROM photo.shoot_sessions s WHERE s.tenant_id=p_tenant
  AND s.id=p_session FOR SHARE;
 IF session_row.id IS NULL OR NOT photo.v2_editor_authorized(
  p_tenant,session_row.project_id,p_actor,p_auth_session) THEN
  RAISE EXCEPTION 'V22_ANALYSIS_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 IF session_row.revision<>p_expected OR session_row.state='archived' THEN
  RAISE EXCEPTION 'V22_ANALYSIS_REVISION_CONFLICT' USING ERRCODE='40001';
 END IF;
 SELECT * INTO snapshot_row FROM photo.snapshots x WHERE x.tenant_id=p_tenant
  AND x.id=p_selection AND x.kind='selection'
  AND x.collection_id=session_row.collection_id
  AND x.principal_type='user' AND x.principal_id=p_actor
  AND x.expires_at>clock_timestamp() FOR SHARE;
 IF snapshot_row.id IS NULL OR snapshot_row.item_count NOT BETWEEN 1 AND 5000 THEN
  RAISE EXCEPTION 'V22_ANALYSIS_SELECTION_INVALID' USING ERRCODE='23514';
 END IF;
 SELECT count(*) INTO item_count FROM photo.snapshot_items i
  JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id
   AND v.id=i.asset_version_id AND v.master_artifact_id=i.artifact_id
   AND v.content_hash=i.sha256
  JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   AND a.project_id=session_row.project_id AND a.deleted_at IS NULL
   AND a.upload_state='verified'
  JOIN photo.artifacts artifact ON artifact.tenant_id=i.tenant_id
   AND artifact.id=i.artifact_id AND artifact.sha256=i.sha256
   AND artifact.state='ready' AND artifact.object_version IS NOT NULL
   AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp())
  WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_selection
   AND i.ordinal BETWEEN 0 AND snapshot_row.item_count-1;
 IF item_count<>snapshot_row.item_count OR EXISTS(
  SELECT 1 FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant
   AND i.snapshot_id=p_selection AND (i.ordinal<0 OR
    i.ordinal>=snapshot_row.item_count)) THEN
  RAISE EXCEPTION 'V22_ANALYSIS_SELECTION_INVALID' USING ERRCODE='23514';
 END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(p_manual) LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' THEN
   RAISE EXCEPTION 'V22_ANALYSIS_REFERENCE_INVALID' USING ERRCODE='22023';
  END IF;
  IF (ref-'previous_analysis_id'-'group_id'-'asset_version_id')<>'{}'::jsonb OR
   NOT (ref ?& ARRAY['previous_analysis_id','group_id','asset_version_id']) OR
   coalesce(ref->>'previous_analysis_id','') !~
    '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' OR
   coalesce(ref->>'group_id','') !~
    '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' OR
   coalesce(ref->>'asset_version_id','') !~
    '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
   RAISE EXCEPTION 'V22_ANALYSIS_REFERENCE_INVALID' USING ERRCODE='22023';
  END IF;
  previous:=(ref->>'previous_analysis_id')::uuid;
  chosen_group:=(ref->>'group_id')::uuid;
  chosen:=(ref->>'asset_version_id')::uuid;
  IF chosen_group=ANY(seen_groups) OR NOT EXISTS(
   SELECT 1 FROM photo.session_analyses analysis
    JOIN photo.scene_groups scene ON scene.tenant_id=analysis.tenant_id
     AND scene.analysis_id=analysis.id
    WHERE analysis.tenant_id=p_tenant AND analysis.project_id=session_row.project_id
     AND analysis.session_id=p_session AND analysis.id=previous AND scene.id=chosen_group)
   OR NOT EXISTS(SELECT 1 FROM photo.scene_members member
    WHERE member.tenant_id=p_tenant AND member.group_id=chosen_group
     AND member.asset_version_id=chosen)
   OR NOT EXISTS(SELECT 1 FROM photo.snapshot_items selected
    WHERE selected.tenant_id=p_tenant AND selected.snapshot_id=p_selection
     AND selected.asset_version_id=chosen) THEN
   RAISE EXCEPTION 'V22_ANALYSIS_REFERENCE_CONFLICT' USING ERRCODE='23514';
  END IF;
  seen_groups:=array_append(seen_groups,chosen_group);
  SELECT jsonb_agg(member.asset_version_id ORDER BY member.snapshot_ordinal,
    member.asset_version_id) INTO prev_members FROM photo.scene_members member
   WHERE member.tenant_id=p_tenant AND member.group_id=chosen_group;
  IF prev_members IS NULL OR EXISTS(
   SELECT 1 FROM photo.scene_members member WHERE member.tenant_id=p_tenant
    AND member.group_id=chosen_group AND NOT EXISTS(
     SELECT 1 FROM photo.snapshot_items selected
      WHERE selected.tenant_id=p_tenant AND selected.snapshot_id=p_selection
       AND selected.asset_version_id=member.asset_version_id)) THEN
   RAISE EXCEPTION 'V22_ANALYSIS_REFERENCE_CONFLICT' USING ERRCODE='23514';
  END IF;
  context_refs:=context_refs || jsonb_build_array(jsonb_build_object(
   'asset_version_id',chosen,'previous_members',prev_members));
 END LOOP;
 RETURN jsonb_build_object('session_id',p_session,'project_id',session_row.project_id,
  'session_revision',session_row.revision::text,'collection_id',session_row.collection_id,
  'selection_snapshot_id',p_selection,'selection_digest',snapshot_row.digest,
  'item_count',snapshot_row.item_count::text,'manual_references',context_refs);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.preflight_v22_analysis(uuid,uuid,bigint,uuid,jsonb,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.preflight_v22_analysis(uuid,uuid,bigint,uuid,jsonb,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.preflight_v22_analysis(uuid,uuid,bigint,uuid,jsonb,uuid,uuid) TO photo_api;
