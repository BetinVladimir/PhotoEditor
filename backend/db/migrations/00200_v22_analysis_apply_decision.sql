-- +goose Up
-- Immutable one-shot decision. Deleting an analysis during the confirmed
-- tenant purge cascades to this row before the child run is removed.
CREATE TABLE photo.v22_analysis_decisions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 analysis_id uuid NOT NULL,
 child_run_id uuid NOT NULL,
 actor_id uuid NOT NULL REFERENCES photo.users(id),
 plan_sha text NOT NULL CHECK(plan_sha ~ '^[0-9a-f]{64}$'),
 recipe_artifact_id uuid NOT NULL,
 recipe_sha text NOT NULL CHECK(recipe_sha ~ '^[0-9a-f]{64}$'),
 decision_sha text NOT NULL CHECK(decision_sha ~ '^[0-9a-f]{64}$'),
 approved_groups uuid[] NOT NULL CHECK(cardinality(approved_groups) BETWEEN 1 AND 5000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(tenant_id,id),
 UNIQUE(tenant_id,analysis_id),
 UNIQUE(tenant_id,child_run_id),
 FOREIGN KEY(tenant_id,project_id,analysis_id)
  REFERENCES photo.session_analyses(tenant_id,project_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,child_run_id) REFERENCES photo.runs(tenant_id,id)
  DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,recipe_artifact_id) REFERENCES photo.artifacts(tenant_id,id)
  DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE photo.v22_analysis_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v22_analysis_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v22_analysis_decisions TO photo_fn_owner
 USING(true) WITH CHECK(true);
REVOKE ALL ON photo.v22_analysis_decisions FROM PUBLIC;
GRANT SELECT,INSERT,DELETE ON photo.v22_analysis_decisions TO photo_fn_owner;
CREATE TRIGGER v22_analysis_decisions_immutable BEFORE UPDATE
 ON photo.v22_analysis_decisions FOR EACH ROW
 EXECUTE FUNCTION photo.guard_v2_immutable();

-- Called as the commit hook of child Apply admission. The child snapshot must
-- equal exactly the union of approved parent snapshot members, including item,
-- version, artifact and content hash; checking only counts would permit swaps.
-- +goose StatementBegin
CREATE FUNCTION photo.finish_v22_analysis_apply(
 p_tenant uuid,p_analysis uuid,p_actor uuid,p_auth_session uuid,
 p_expected_revision bigint,p_plan_sha text,p_recipe_artifact uuid,
 p_recipe_sha text,p_decision_sha text,p_groups uuid[],p_child uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE analysis photo.session_analyses%ROWTYPE;
 parent photo.runs%ROWTYPE; child photo.runs%ROWTYPE;
 session_row photo.shoot_sessions%ROWTYPE; expected_count bigint;
 event_cursor bigint; event_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_analysis IS NULL OR p_actor IS NULL
  OR p_auth_session IS NULL OR p_child IS NULL OR p_recipe_artifact IS NULL
  OR p_expected_revision NOT BETWEEN 1 AND 9007199254740990
  OR coalesce(p_plan_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_recipe_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_decision_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(cardinality(p_groups),0) NOT BETWEEN 1 AND 5000
  OR EXISTS(SELECT 1 FROM unnest(p_groups) g WHERE g IS NULL
   OR g='00000000-0000-0000-0000-000000000000'::uuid)
  OR (SELECT count(DISTINCT g) FROM unnest(p_groups) g)<>cardinality(p_groups) THEN
  RAISE EXCEPTION 'V22_APPLY_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO analysis FROM photo.session_analyses a
  WHERE a.tenant_id=p_tenant AND a.id=p_analysis;
 IF analysis.id IS NULL OR NOT photo.v2_editor_authorized(
  p_tenant,analysis.project_id,p_actor,p_auth_session) THEN
  RAISE EXCEPTION 'V22_APPLY_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT r.* INTO parent FROM photo.runs r JOIN photo.v2_workflows w
  ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  WHERE w.tenant_id=p_tenant AND w.id=analysis.workflow_id
   AND w.workflow_type='session' AND w.project_id=analysis.project_id
   AND w.input_snapshot_id=analysis.snapshot_id FOR UPDATE OF r;
 SELECT * INTO session_row FROM photo.shoot_sessions s
  WHERE s.tenant_id=p_tenant AND s.id=analysis.session_id
   AND s.project_id=analysis.project_id;
 IF parent.id IS NULL OR parent.kind<>'v2' OR parent.status<>'waiting_review'
  OR parent.cancel_requested OR NOT parent.graph_sealed
  OR parent.revision<>p_expected_revision OR session_row.id IS NULL
  OR analysis.plan_body_digest IS NULL
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=analysis.plan_artifact_id
    AND a.sha256=p_plan_sha AND a.state='ready'
    AND a.object_version IS NOT NULL)
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=p_recipe_artifact
    AND a.sha256=p_recipe_sha AND a.state='ready'
    AND a.media_type='application/json' AND a.object_version IS NOT NULL)
  OR EXISTS(SELECT 1 FROM photo.v22_analysis_decisions d
   WHERE d.tenant_id=p_tenant AND d.analysis_id=p_analysis) THEN
  RAISE EXCEPTION 'V22_APPLY_STALE' USING ERRCODE='40001';
 END IF;
 SELECT count(*) INTO expected_count FROM photo.scene_members m
  JOIN photo.scene_groups g ON g.tenant_id=m.tenant_id
   AND g.project_id=m.project_id AND g.id=m.group_id
  WHERE g.tenant_id=p_tenant AND g.analysis_id=p_analysis
   AND g.id=ANY(p_groups);
 IF expected_count<1 OR expected_count>5000 OR
  (SELECT count(*) FROM photo.scene_groups g WHERE g.tenant_id=p_tenant
   AND g.analysis_id=p_analysis AND g.id=ANY(p_groups))<>cardinality(p_groups) THEN
  RAISE EXCEPTION 'V22_APPLY_GROUPS_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO child FROM photo.runs r WHERE r.tenant_id=p_tenant
  AND r.id=p_child FOR UPDATE;
 IF child.id IS NULL OR child.kind<>'apply' OR child.status<>'queued'
  OR child.cancel_requested OR child.created_by<>p_actor
  OR child.config_snapshot->>'analysis_decision_sha' IS DISTINCT FROM p_decision_sha
  OR child.config_snapshot->>'source_collection_id' IS DISTINCT FROM
   session_row.collection_id::text
  OR child.config_snapshot->>'scope' IS DISTINCT FROM 'selected'
  OR child.config_snapshot->>'input_snapshot_id' IS DISTINCT FROM
   child.input_snapshot_id::text
  OR (CASE WHEN jsonb_typeof(child.config_snapshot->'item_overrides')='array'
   THEN jsonb_array_length(child.config_snapshot->'item_overrides')
   ELSE -1 END)<>expected_count
  OR NOT EXISTS(SELECT 1 FROM photo.apply_builds b WHERE b.tenant_id=p_tenant
   AND b.run_id=p_child AND b.item_count=expected_count AND b.graph_version=4)
  OR NOT EXISTS(SELECT 1 FROM photo.collections output
   WHERE output.tenant_id=p_tenant AND output.id=child.output_collection_id
    AND output.project_id=analysis.project_id
    AND output.source_collection_id=session_row.collection_id
    AND output.source_run_id=child.id)
  OR NOT EXISTS(SELECT 1 FROM photo.snapshots s WHERE s.tenant_id=p_tenant
   AND s.id=child.input_snapshot_id AND s.kind='selection'
   AND s.collection_id=session_row.collection_id
   AND s.item_count=expected_count AND s.principal_type='user'
   AND s.principal_id=p_actor
   AND child.config_snapshot->>'input_snapshot_digest'=s.digest
   AND child.config_snapshot->>'source_revision'=s.source_revision::text
   AND s.source_revision=(SELECT parent_snapshot.source_revision
    FROM photo.snapshots parent_snapshot WHERE parent_snapshot.tenant_id=p_tenant
     AND parent_snapshot.id=analysis.snapshot_id))
  OR (SELECT count(*) FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant
   AND i.snapshot_id=child.input_snapshot_id)<>expected_count THEN
  RAISE EXCEPTION 'V22_APPLY_CHILD_INVALID' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM photo.snapshot_items item
  WHERE item.tenant_id=p_tenant AND item.snapshot_id=child.input_snapshot_id
   AND (SELECT count(*) FROM jsonb_array_elements(
    child.config_snapshot->'item_overrides') override_entry
    WHERE override_entry->>'item_id'=item.item_id::text
     AND override_entry->>'asset_version_id'=item.asset_version_id::text
     AND override_entry->>'input_sha256'=item.sha256
     AND override_entry->'body'->>'input_sha256'=item.sha256)<>1) THEN
  RAISE EXCEPTION 'V22_APPLY_OVERRIDE_BINDING_INVALID' USING ERRCODE='23514';
 END IF;
 IF EXISTS(
  SELECT 1 FROM photo.scene_members m
  JOIN photo.scene_groups g ON g.tenant_id=m.tenant_id AND g.id=m.group_id
  JOIN photo.snapshot_items source ON source.tenant_id=m.tenant_id
   AND source.snapshot_id=analysis.snapshot_id
   AND source.ordinal=m.snapshot_ordinal
  LEFT JOIN photo.snapshot_items target ON target.tenant_id=p_tenant
   AND target.snapshot_id=child.input_snapshot_id
   AND target.item_id=source.item_id
   AND target.asset_version_id=source.asset_version_id
   AND target.artifact_id=source.artifact_id AND target.sha256=source.sha256
  WHERE g.tenant_id=p_tenant AND g.analysis_id=p_analysis
   AND g.id=ANY(p_groups) AND target.id IS NULL
 ) OR EXISTS(
  SELECT 1 FROM photo.snapshot_items target
  LEFT JOIN photo.snapshot_items source ON source.tenant_id=p_tenant
   AND source.snapshot_id=analysis.snapshot_id
   AND source.item_id=target.item_id
   AND source.asset_version_id=target.asset_version_id
   AND source.artifact_id=target.artifact_id AND source.sha256=target.sha256
  LEFT JOIN photo.scene_members m ON m.tenant_id=p_tenant
   AND m.snapshot_ordinal=source.ordinal AND m.asset_version_id=source.asset_version_id
  LEFT JOIN photo.scene_groups g ON g.tenant_id=p_tenant AND g.id=m.group_id
   AND g.analysis_id=p_analysis AND g.id=ANY(p_groups)
  WHERE target.tenant_id=p_tenant AND target.snapshot_id=child.input_snapshot_id
   AND g.id IS NULL
 ) THEN
  RAISE EXCEPTION 'V22_APPLY_SOURCE_MISMATCH' USING ERRCODE='23514';
 END IF;
 INSERT INTO photo.v22_analysis_decisions(tenant_id,project_id,analysis_id,
  child_run_id,actor_id,plan_sha,recipe_artifact_id,recipe_sha,decision_sha,
  approved_groups)
 VALUES(p_tenant,analysis.project_id,p_analysis,p_child,p_actor,p_plan_sha,
  p_recipe_artifact,p_recipe_sha,p_decision_sha,p_groups);
 UPDATE photo.runs SET status='completed',revision=revision+1
  WHERE tenant_id=p_tenant AND id=parent.id AND revision=p_expected_revision;
 IF NOT FOUND THEN RAISE EXCEPTION 'V22_APPLY_REVISION_CONFLICT' USING ERRCODE='40001'; END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0)
  ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
  WHERE tenant_id=p_tenant AND last_cursor<9007199254740991
  RETURNING last_cursor INTO event_cursor;
 IF event_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514'; END IF;
 event_id:=gen_random_uuid();
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,event_cursor,parent.id,p_actor,'principal','v2.run.completed',
  jsonb_build_object('run_id',parent.id,'status','completed',
   'child_run_id',p_child,'analysis_id',p_analysis));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','v2.run.completed',
  jsonb_build_object('schema_version',1,'event_id',event_id,
   'type','v2.run.completed','tenant_id',p_tenant,'run_id',parent.id,
   'status','completed','child_run_id',p_child));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_v22_analysis_apply(uuid,uuid,uuid,uuid,bigint,text,
 uuid,text,text,uuid[],uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_v22_analysis_apply(uuid,uuid,uuid,uuid,bigint,text,
 uuid,text,text,uuid[],uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_v22_analysis_apply(uuid,uuid,uuid,uuid,bigint,text,
 uuid,text,text,uuid[],uuid) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
