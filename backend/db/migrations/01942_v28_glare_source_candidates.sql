-- +goose Up
-- Select confirmed source faces for the active frame without returning person IDs.
-- +goose StatementBegin
CREATE FUNCTION photo.list_v28_glare_source_candidates(p_tenant uuid,p_project uuid,
 p_shoot uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb,p_after uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE collection_id uuid;items jsonb:='[]'::jsonb;row record;
 seen integer:=0;cursor_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_project IS NULL OR p_shoot IS NULL OR p_snapshot IS NULL
 OR p_actor IS NULL OR p_session IS NULL OR jsonb_typeof(p_base) IS DISTINCT FROM 'object'
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
    IS DISTINCT FROM true THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.snapshots s WHERE s.tenant_id=p_tenant AND s.id=p_snapshot
  AND s.kind='v2' AND s.principal_type='user' AND s.principal_id=p_actor
  AND (s.expires_at IS NULL OR s.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT session_row.collection_id INTO collection_id FROM photo.shoot_sessions session_row
 WHERE session_row.tenant_id=p_tenant AND session_row.project_id=p_project
  AND session_row.id=p_shoot AND session_row.state IN('open','closed') FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 FOR row IN SELECT source.id observation_id,evidence.id evidence_id,
  evidence.sha256 evidence_sha
 FROM photo.person_observations source
 JOIN photo.project_persons person ON person.tenant_id=source.tenant_id
  AND person.project_id=source.project_id AND person.id=source.person_id
  AND person.state='active'
 JOIN photo.asset_versions v ON v.tenant_id=source.tenant_id
  AND v.id=source.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.collection_items ci ON ci.tenant_id=asset.tenant_id
  AND ci.collection_id=collection_id AND ci.asset_id=asset.id
  AND ci.asset_version_id=v.id
 JOIN photo.artifacts image ON image.tenant_id=v.tenant_id
  AND image.id=v.master_artifact_id AND image.sha256=v.content_hash
 JOIN photo.artifacts evidence ON evidence.tenant_id=source.tenant_id
  AND evidence.id=source.evidence_artifact_id
 WHERE source.tenant_id=p_tenant AND source.project_id=p_project
  AND source.confirmed_by IS NOT NULL AND (p_after IS NULL OR source.id>p_after)
  AND v.id=(p_base->>'asset_version_id')::uuid
  AND image.id=(p_base->'source'->>'artifact_id')::uuid
  AND image.sha256=p_base->'source'->>'sha256'
  AND EXISTS(SELECT 1 FROM photo.snapshot_items snapshot_item
   WHERE snapshot_item.tenant_id=p_tenant AND snapshot_item.snapshot_id=p_snapshot
   AND snapshot_item.asset_version_id=v.id AND snapshot_item.artifact_id=image.id
   AND snapshot_item.sha256=image.sha256)
  AND (SELECT count(*) FROM photo.consent_grants g
   WHERE g.tenant_id=p_tenant AND g.project_id=p_project
    AND g.person_id=person.id AND g.purpose='identity_matching'
    AND g.status='granted' AND g.epoch=person.consent_epoch)=1
  AND v.review_state='accepted' AND asset.project_id=p_project
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
  AND image.state='ready' AND image.verified_at IS NOT NULL
  AND image.object_version NOT IN('','null')
  AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp())
  AND evidence.state='ready' AND evidence.verified_at IS NOT NULL
  AND evidence.media_type='application/json' AND evidence.bytes BETWEEN 2 AND 1048576
  AND evidence.object_version NOT IN('','null')
  AND (evidence.expires_at IS NULL OR evidence.expires_at>clock_timestamp())
 ORDER BY source.id LIMIT 101 FOR SHARE OF source,person,v,asset,ci,image,evidence
 LOOP
  seen:=seen+1;
  IF seen>100 THEN EXIT;END IF;
  cursor_id:=row.observation_id;
  items:=items||jsonb_build_array(jsonb_build_object(
   'observation_id',row.observation_id,
   'source_faces',jsonb_build_object('artifact_id',row.evidence_id,
    'sha256',row.evidence_sha)));
 END LOOP;
 RETURN jsonb_build_object('items',items,
  'next_cursor',CASE WHEN seen>100 THEN cursor_id ELSE NULL END);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.list_v28_glare_source_candidates(uuid,uuid,uuid,uuid,uuid,
 uuid,jsonb,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.list_v28_glare_source_candidates(uuid,uuid,uuid,
 uuid,uuid,uuid,jsonb,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.list_v28_glare_source_candidates(uuid,uuid,uuid,
 uuid,uuid,uuid,jsonb,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
