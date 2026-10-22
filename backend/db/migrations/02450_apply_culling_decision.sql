-- +goose Up
-- Human selection never changes the frozen input snapshot or deletes sources.
-- Scheduler alone may resume the cull_review phase under its existing lease.
-- +goose StatementBegin
CREATE FUNCTION photo.decide_apply_culling_review(p_tenant uuid,p_review uuid,p_expected_revision bigint,
 p_candidate_hash text,p_actor uuid,p_session uuid,p_selected uuid[],p_confirmed uuid[],p_decision_hash text,p_request uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE c photo.apply_culling_reviews%ROWTYPE;b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;v photo.reviews%ROWTYPE;
 v_project uuid;selected_ids uuid[];confirmed_ids uuid[];selected_wire text;confirmed_wire text;decision_wire text;v_selection jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_actor IS NULL OR p_session IS NULL OR p_review IS NULL OR p_request IS NULL
  OR p_expected_revision IS NULL OR p_expected_revision NOT BETWEEN 1 AND 9007199254740990
  OR coalesce(p_candidate_hash,'')!~'^[0-9a-f]{64}$' OR coalesce(p_decision_hash,'')!~'^[0-9a-f]{64}$'
  OR p_selected IS NULL OR p_confirmed IS NULL OR cardinality(p_selected) NOT BETWEEN 1 AND 5000
  OR cardinality(p_confirmed)>5000 OR array_position(p_selected,NULL) IS NOT NULL OR array_position(p_confirmed,NULL) IS NOT NULL
  OR '00000000-0000-0000-0000-000000000000'::uuid=ANY(p_selected||p_confirmed)
 THEN RAISE EXCEPTION 'INVALID_CULLING_DECISION' USING ERRCODE='22023';END IF;
 SELECT * INTO c FROM photo.apply_culling_reviews WHERE tenant_id=p_tenant AND review_id=p_review;
 IF NOT FOUND THEN RETURN false;END IF;
 -- Identical ordering to scheduler registration/resume and cancellation.
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=c.run_id FOR UPDATE;
 IF NOT FOUND OR b.graph_version<>4 OR b.phase<>'cull_review' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=c.run_id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'apply' OR r.cancel_requested OR r.status NOT IN('running','waiting_review')
  OR r.input_snapshot_id IS DISTINCT FROM c.snapshot_id OR r.config_snapshot->>'input_snapshot_digest' IS DISTINCT FROM c.snapshot_digest THEN RETURN false;END IF;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review FOR UPDATE;
 IF NOT FOUND OR v.run_id<>c.run_id OR v.kind<>'culling' OR v.state<>'pending' OR v.revision<>p_expected_revision
  OR v.candidate_hash<>p_candidate_hash OR v.candidate_hash IS DISTINCT FROM c.projection->'proposal'->>'sha256' THEN RETURN false;END IF;
 SELECT col.project_id INTO v_project FROM photo.snapshots snap JOIN photo.collections col ON col.tenant_id=snap.tenant_id AND col.id=snap.collection_id
 WHERE snap.tenant_id=p_tenant AND snap.id=c.snapshot_id AND snap.digest=c.snapshot_digest
  AND snap.item_count=jsonb_array_length(c.projection->'items') AND snap.item_count BETWEEN 1 AND 5000;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,v_project,p_actor,p_session) THEN
  RAISE EXCEPTION 'CULLING_EDITOR_REQUIRED' USING ERRCODE='42501';END IF;
 -- Stabilize active actor and source authority through the final UPDATE.
 PERFORM id FROM photo.users WHERE id=p_actor FOR SHARE;
 PERFORM id FROM photo.sessions WHERE id=p_session AND user_id=p_actor FOR SHARE;
 PERFORM user_id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=v_project FOR SHARE;
 PERFORM user_id FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=v_project AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=c.snapshot_id FOR SHARE;
 PERFORM av.id FROM photo.snapshot_items si JOIN photo.asset_versions av ON av.tenant_id=si.tenant_id AND av.id=si.asset_version_id
 JOIN photo.assets a ON a.tenant_id=av.tenant_id AND a.id=av.asset_id
 JOIN photo.artifacts ar ON ar.tenant_id=si.tenant_id AND ar.id=si.artifact_id
 WHERE si.tenant_id=p_tenant AND si.snapshot_id=c.snapshot_id ORDER BY av.id FOR SHARE OF av,a,ar;
 IF NOT photo.v2_editor_authorized(p_tenant,v_project,p_actor,p_session) THEN RAISE EXCEPTION 'CULLING_EDITOR_REQUIRED' USING ERRCODE='42501';END IF;
 IF c.projection->'never_delete' IS DISTINCT FROM 'true'::jsonb
  OR jsonb_array_length(c.projection->'items')<>(SELECT count(DISTINCT target->>'item_id') FROM jsonb_array_elements(c.projection->'items') target)
  OR EXISTS(SELECT 1 FROM jsonb_array_elements(c.projection->'items') target WHERE NOT EXISTS(
   SELECT 1 FROM photo.snapshot_items si JOIN photo.asset_versions av ON av.tenant_id=si.tenant_id AND av.id=si.asset_version_id AND av.master_artifact_id=si.artifact_id AND av.content_hash=si.sha256
   JOIN photo.assets a ON a.tenant_id=av.tenant_id AND a.id=av.asset_id AND a.project_id=v_project AND a.deleted_at IS NULL AND a.upload_state='verified'
   JOIN photo.artifacts ar ON ar.tenant_id=si.tenant_id AND ar.id=si.artifact_id AND ar.sha256=si.sha256 AND ar.state='ready' AND ar.verified_at IS NOT NULL
    AND ar.object_version IS NOT NULL AND ar.object_version NOT IN('','null') AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
   WHERE si.tenant_id=p_tenant AND si.snapshot_id=c.snapshot_id AND si.item_id::text=target->>'item_id'
    AND si.asset_version_id::text=target->>'asset_version_id' AND a.id::text=target->>'asset_id'))
 THEN RETURN false;END IF;
 PERFORM a.id FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id::text IN(c.projection->'groups'->>'artifact_id',c.projection->'proposal'->>'artifact_id')
 ORDER BY a.id FOR SHARE;
 IF EXISTS(SELECT 1 FROM (VALUES(c.projection->'groups'),(c.projection->'proposal')) pin(ref)
  WHERE NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id::text=pin.ref->>'artifact_id'
   AND a.sha256=pin.ref->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN RETURN false;END IF;
 SELECT coalesce(array_agg(x ORDER BY x::text COLLATE "C"),ARRAY[]::uuid[]) INTO selected_ids FROM unnest(p_selected) x;
 SELECT coalesce(array_agg(x ORDER BY x::text COLLATE "C"),ARRAY[]::uuid[]) INTO confirmed_ids FROM unnest(p_confirmed) x;
 IF cardinality(selected_ids)<>(SELECT count(DISTINCT x) FROM unnest(selected_ids) x)
  OR cardinality(confirmed_ids)<>(SELECT count(DISTINCT x) FROM unnest(confirmed_ids) x)
  OR NOT confirmed_ids<@selected_ids OR EXISTS(SELECT 1 FROM unnest(selected_ids) id WHERE NOT EXISTS(
   SELECT 1 FROM jsonb_array_elements(c.projection->'items') target WHERE target->>'item_id'=id::text))
  OR EXISTS(SELECT 1 FROM jsonb_array_elements(c.projection->'items') target WHERE (target->>'item_id')::uuid=ANY(selected_ids)
   AND (target->>'eye_review')::boolean AND NOT (target->>'item_id')::uuid=ANY(confirmed_ids))
 THEN RAISE EXCEPTION 'INVALID_CULLING_SELECTION' USING ERRCODE='22023';END IF;
 -- Only UUIDs, hex digests, literals and sorted UUID arrays enter this JCS wire.
 SELECT '['||coalesce(string_agg(to_json(x::text)::text,',' ORDER BY x::text COLLATE "C"),'')||']' INTO selected_wire FROM unnest(selected_ids) x;
 SELECT '['||coalesce(string_agg(to_json(x::text)::text,',' ORDER BY x::text COLLATE "C"),'')||']' INTO confirmed_wire FROM unnest(confirmed_ids) x;
 decision_wire:=format('{"actor_id":"%s","candidate_hash":"%s","confirmed_eyes_item_ids":%s,"never_delete":true,"review_id":"%s","run_id":"%s","selected_item_ids":%s,"snapshot_digest":"%s","snapshot_id":"%s","tenant_id":"%s","version":"culling_decision_v1"}',p_actor,p_candidate_hash,confirmed_wire,p_review,c.run_id,selected_wire,c.snapshot_digest,c.snapshot_id,p_tenant);
 IF encode(sha256(convert_to(decision_wire,'UTF8')),'hex')<>p_decision_hash THEN RAISE EXCEPTION 'CULLING_DECISION_HASH' USING ERRCODE='22023';END IF;
 v_selection:=jsonb_build_object('selected_item_ids',to_jsonb(selected_ids),'confirmed_eyes_item_ids',to_jsonb(confirmed_ids),'never_delete',true);
 UPDATE photo.reviews SET state='decided',revision=revision+1,decision='accept',decision_route='selection',decision_hash=p_decision_hash,
  selection=v_selection,decided_by=p_actor,decided_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=p_review;
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES(p_tenant,'user',p_actor,'review.culling.decided','review',p_review,p_request,jsonb_build_object('run_id',c.run_id,'snapshot_id',c.snapshot_id,'snapshot_digest',c.snapshot_digest,'decision_hash',p_decision_hash,'selected_count',cardinality(selected_ids),'never_delete',true));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.decide_apply_culling_review(uuid,uuid,bigint,text,uuid,uuid,uuid[],uuid[],text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.decide_apply_culling_review(uuid,uuid,bigint,text,uuid,uuid,uuid[],uuid[],text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.decide_apply_culling_review(uuid,uuid,bigint,text,uuid,uuid,uuid[],uuid[],text,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
