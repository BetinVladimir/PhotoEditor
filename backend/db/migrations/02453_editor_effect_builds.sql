-- +goose Up
-- Dedicated single-source mode shares Apply tasks and reviews without creating
-- product collections or changing the existing V2 batch cardinality contract.
ALTER TABLE photo.apply_builds ALTER COLUMN source_collection_id DROP NOT NULL;
CREATE TABLE photo.editor_effect_builds(
 tenant_id uuid NOT NULL,
 run_id uuid NOT NULL,
 project_id uuid NOT NULL,
 actor_id uuid NOT NULL,
 session_id uuid NOT NULL,
 asset_version_id uuid NOT NULL,
 source_artifact_id uuid NOT NULL,
 source_sha text NOT NULL CHECK(source_sha ~ '^[0-9a-f]{64}$'),
 source_canvas_hash text NOT NULL CHECK(source_canvas_hash ~ '^[0-9a-f]{64}$'),
 frozen_recipe_artifact_id uuid NOT NULL,
 frozen_recipe_sha text NOT NULL CHECK(frozen_recipe_sha ~ '^[0-9a-f]{64}$'),
 frozen_document_id uuid NOT NULL,
 draft_document_id uuid NOT NULL,
 draft_revision bigint NOT NULL CHECK(draft_revision BETWEEN 1 AND 9007199254740991),
 executed_style_artifact_id uuid,
 executed_style_sha text CHECK(executed_style_sha ~ '^[0-9a-f]{64}$'),
 compilation_sha text NOT NULL CHECK(compilation_sha ~ '^[0-9a-f]{64}$'),
 state text NOT NULL DEFAULT 'building' CHECK(state IN('building','effects_ready','sealing','sealed','composing','complete','failed')),
 processed_source_artifact_id uuid,
 processed_source_sha text CHECK(processed_source_sha ~ '^[0-9a-f]{64}$'),
 processed_source_canvas_hash text CHECK(processed_source_canvas_hash ~ '^[0-9a-f]{64}$'),
 producer_task_id uuid,
 producer_result_id uuid,
 producer_attempt int,
 producer_fence bigint,
 quality_artifact_id uuid,
 quality_sha text CHECK(quality_sha ~ '^[0-9a-f]{64}$'),
 quality_task_id uuid,
 quality_result_id uuid,
 result_artifact_id uuid,
 result_sha text CHECK(result_sha ~ '^[0-9a-f]{64}$'),
 final_run_id uuid,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,run_id),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,source_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,frozen_recipe_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 CHECK((executed_style_artifact_id IS NULL)=(executed_style_sha IS NULL)),
 CHECK((processed_source_artifact_id IS NULL)=(processed_source_sha IS NULL)),
 CHECK((quality_artifact_id IS NULL)=(quality_sha IS NULL)),
 CHECK((result_artifact_id IS NULL)=(result_sha IS NULL))
);
ALTER TABLE photo.editor_effect_builds ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.editor_effect_builds FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.editor_effect_builds TO photo_api
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.editor_effect_builds TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.editor_effect_builds FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT SELECT,INSERT ON photo.editor_effect_builds TO photo_api;
GRANT SELECT,UPDATE,DELETE ON photo.editor_effect_builds TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.editor_effect_origin_scope(p_tenant uuid,p_project uuid,p_version uuid,p_source uuid,p_source_sha text,
 p_recipe uuid,p_recipe_sha text,p_frozen uuid,p_draft uuid,p_revision bigint,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid,false)
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND EXISTS(SELECT FROM photo.v2_draft_uploads u
  JOIN photo.asset_versions v ON v.tenant_id=u.tenant_id AND v.id=u.asset_version_id
  JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id AND a.project_id=p_project
  WHERE u.tenant_id=p_tenant AND u.document_id=p_frozen AND u.asset_version_id=p_version
   AND u.artifact_id=p_recipe AND u.sha256=p_recipe_sha
   AND u.render_draft_document_id=p_draft AND u.render_draft_revision=p_revision
   AND u.render_source_id=p_source AND u.render_source_sha=p_source_sha
   AND v.master_artifact_id=p_source AND v.content_hash=p_source_sha
   AND a.deleted_at IS NULL AND a.upload_state='verified' AND v.review_state='accepted'
   AND photo.v26_render_recipe_scope(p_tenant,p_project,p_version,p_recipe,p_recipe_sha,p_actor,p_session))
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.editor_effect_origin_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,bigint,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_origin_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,bigint,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_origin_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,bigint,uuid,uuid) TO photo_api;
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 needle:=' DELETE FROM photo.editor_recipe_bridges WHERE tenant_id=j.tenant_id;';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'EDITOR_EFFECT_PURGE_ANCHOR_MISSING';END IF;
 EXECUTE replace(definition,needle,' DELETE FROM photo.editor_effect_builds WHERE tenant_id=j.tenant_id;'||needle);
END $$;
-- +goose StatementEnd
-- The ordinary Apply input reader is already collection independent. Keep its
-- return contract, but fence dedicated-source reads by the frozen origin.
ALTER FUNCTION photo.apply_v4_inputs_with_metadata(uuid,uuid,uuid)
 RENAME TO apply_v4_inputs_before_editor_origin;
REVOKE ALL ON FUNCTION photo.apply_v4_inputs_before_editor_origin(uuid,uuid,uuid) FROM PUBLIC,photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.apply_v4_inputs_with_metadata(p_tenant uuid,p_run uuid,p_token uuid)
RETURNS TABLE(ordinal bigint,item_id uuid,asset_id uuid,asset_version_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text,payload jsonb,config_snapshot jsonb,style_artifact_id uuid,style_sha256 text,style_bytes bigint,asset_metadata jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE origin photo.editor_effect_builds%ROWTYPE; old_tenant text;
BEGIN
 SELECT * INTO origin FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_run;
 IF FOUND THEN
  old_tenant:=current_setting('app.tenant_id',true);
  PERFORM set_config('app.tenant_id',p_tenant::text,true);
  IF NOT photo.editor_effect_origin_scope(p_tenant,origin.project_id,origin.asset_version_id,origin.source_artifact_id,origin.source_sha,
   origin.frozen_recipe_artifact_id,origin.frozen_recipe_sha,origin.frozen_document_id,origin.draft_document_id,origin.draft_revision,origin.actor_id,origin.session_id) THEN
   PERFORM set_config('app.tenant_id',coalesce(old_tenant,''),true);RETURN;
  END IF;
  PERFORM set_config('app.tenant_id',coalesce(old_tenant,''),true);
 ELSIF EXISTS(SELECT FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run AND source_collection_id IS NULL) THEN RETURN;
 END IF;
 RETURN QUERY SELECT * FROM photo.apply_v4_inputs_before_editor_origin(p_tenant,p_run,p_token);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_v4_inputs_with_metadata(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v4_inputs_with_metadata(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.apply_v4_inputs_with_metadata(uuid,uuid,uuid) TO photo_scheduler;
-- Both sides are inserted in one admission transaction. A deferred check
-- prevents a public collection Apply from accidentally acquiring this mode.
-- +goose StatementBegin
CREATE FUNCTION photo.check_editor_effect_source_mode() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT FROM photo.apply_builds b WHERE b.tenant_id=NEW.tenant_id AND b.run_id=NEW.run_id AND b.source_collection_id IS NULL
  AND NOT EXISTS(SELECT FROM photo.editor_effect_builds e WHERE e.tenant_id=b.tenant_id AND e.run_id=b.run_id)) THEN
  RAISE EXCEPTION 'EDITOR_EFFECT_ORIGIN_REQUIRED' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT FROM photo.editor_effect_builds e JOIN photo.apply_builds b ON b.tenant_id=e.tenant_id AND b.run_id=e.run_id
  JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  WHERE e.tenant_id=NEW.tenant_id AND e.run_id=NEW.run_id
   AND (b.source_collection_id IS NOT NULL OR r.output_collection_id IS NOT NULL OR b.item_count<>1 OR b.graph_version<>4)) THEN
  RAISE EXCEPTION 'EDITOR_EFFECT_SOURCE_MODE_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.check_editor_effect_source_mode() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.check_editor_effect_source_mode() FROM PUBLIC;
CREATE CONSTRAINT TRIGGER editor_effect_source_mode_build AFTER INSERT OR UPDATE ON photo.apply_builds
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION photo.check_editor_effect_source_mode();
CREATE CONSTRAINT TRIGGER editor_effect_source_mode_origin AFTER INSERT OR UPDATE ON photo.editor_effect_builds
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION photo.check_editor_effect_source_mode();
-- Preserve the existing atomic collection adopter; dedicated Editor runs
-- retain an intermediate canonical target for the later sealed V2 compose.
ALTER FUNCTION photo.adopt_apply_results(uuid,uuid,uuid) RENAME TO adopt_apply_results_before_editor_origin;
REVOKE ALL ON FUNCTION photo.adopt_apply_results_before_editor_origin(uuid,uuid,uuid) FROM PUBLIC,photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_apply_results(p_tenant uuid,p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;o photo.editor_effect_builds%ROWTYPE;
 n photo.apply_nodes%ROWTYPE;t photo.tasks%ROWTYPE;qt photo.task_results%ROWTYPE;
 producer photo.tasks%ROWTYPE;pr photo.task_results%ROWTYPE;a photo.artifacts%ROWTYPE;quality photo.artifacts%ROWTYPE;old_tenant text;
BEGIN
 IF NOT EXISTS(SELECT FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_run) THEN
  RETURN photo.adopt_apply_results_before_editor_origin(p_tenant,p_run,p_token);
 END IF;
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO o FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF b.phase<>'adopt' OR b.graph_version<>4 OR b.item_count<>1 OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp()
  OR r.cancel_requested OR r.status NOT IN('queued','running') OR NOT r.graph_sealed OR o.state<>'building' THEN RETURN false;END IF;
 old_tenant:=current_setting('app.tenant_id',true);PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF NOT photo.editor_effect_origin_scope(p_tenant,o.project_id,o.asset_version_id,o.source_artifact_id,o.source_sha,o.frozen_recipe_artifact_id,o.frozen_recipe_sha,o.frozen_document_id,o.draft_document_id,o.draft_revision,o.actor_id,o.session_id) THEN
  PERFORM set_config('app.tenant_id',coalesce(old_tenant,''),true);RETURN false;
 END IF;
 PERFORM set_config('app.tenant_id',coalesce(old_tenant,''),true);
 IF (SELECT count(*) FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND phase='quality')<>1 THEN RETURN false;END IF;
 SELECT * INTO n FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND phase='quality';
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id FOR UPDATE;
 IF t.state<>'succeeded' OR t.canonical_result_id IS NULL OR n.operation<>'quality.check' OR n.params->>'target_kind'<>'working'
  OR n.params->>'stage'<>'master' OR NOT(t.inputs @> jsonb_build_array(n.params->'target')) OR n.source_version_id<>o.asset_version_id THEN RETURN false;END IF;
 SELECT * INTO qt FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id;
 SELECT x.* INTO quality FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.created_by_task_id=t.id AND x.produced_attempt=qt.attempt
  AND x.produced_fence=qt.fencing_token AND x.output_role='quality_report' AND x.state='ready' AND x.object_version IS NOT NULL
  AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) AND qt.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',x.id,'sha256',x.sha256));
 SELECT pt.* INTO producer FROM photo.tasks pt JOIN photo.apply_nodes pn ON pn.tenant_id=pt.tenant_id AND pn.id=pt.id AND pn.run_id=p_run
  JOIN photo.apply_v4_stages ps ON ps.tenant_id=pn.tenant_id AND ps.run_id=pn.run_id AND ps.phase=pn.phase
  JOIN photo.apply_v4_stages qs ON qs.tenant_id=n.tenant_id AND qs.run_id=n.run_id AND qs.phase='quality'
  WHERE pt.tenant_id=p_tenant AND pt.run_id=p_run AND pt.state='succeeded' AND pt.canonical_result_id IS NOT NULL
   AND pn.source_item_id=n.source_item_id AND pn.ordinal=n.ordinal AND ps.stage_index<qs.stage_index
   AND EXISTS(SELECT FROM photo.artifacts candidate WHERE candidate.tenant_id=pt.tenant_id AND candidate.created_by_task_id=pt.id
    AND candidate.id=(n.params->'target'->>'artifact_id')::uuid AND candidate.sha256=n.params->'target'->>'sha256' AND candidate.output_role='working_image')
  ORDER BY ps.stage_index DESC LIMIT 1;
 IF producer.id IS NULL OR quality.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO pr FROM photo.task_results WHERE tenant_id=p_tenant AND id=producer.canonical_result_id AND task_id=producer.id;
 SELECT x.* INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.id=(n.params->'target'->>'artifact_id')::uuid
  AND x.sha256=n.params->'target'->>'sha256' AND x.created_by_task_id=producer.id AND x.produced_attempt=pr.attempt AND x.produced_fence=pr.fencing_token
  AND x.output_role='working_image' AND x.state='ready' AND x.object_version IS NOT NULL AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp())
  AND pr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',x.id,'sha256',x.sha256));
 IF a.id IS NULL THEN RETURN false;END IF;
 UPDATE photo.editor_effect_builds SET state='effects_ready',processed_source_artifact_id=a.id,processed_source_sha=a.sha256,
  producer_task_id=producer.id,producer_result_id=pr.id,producer_attempt=pr.attempt,producer_fence=pr.fencing_token,
  quality_artifact_id=quality.id,quality_sha=quality.sha256,quality_task_id=t.id,quality_result_id=qt.id,updated_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.apply_builds SET phase='complete',lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run;
 -- This completes the staged sub-run only. It does not adopt a client version
 -- or release output reservation before the sealed final compose handoff.
 UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_apply_results(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_apply_results(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_apply_results(uuid,uuid,uuid) TO photo_scheduler;
-- Extend the already validated culling decision CAS to dedicated source mode.
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;replacement text;BEGIN
 SELECT pg_get_functiondef('photo.decide_apply_culling_review(uuid,uuid,bigint,text,uuid,uuid,uuid[],uuid[],text,uuid)'::regprocedure) INTO definition;
 needle:='SELECT col.project_id INTO v_project FROM photo.snapshots snap JOIN photo.collections col ON col.tenant_id=snap.tenant_id AND col.id=snap.collection_id';
 replacement:='SELECT coalesce(col.project_id,effects.project_id) INTO v_project FROM photo.snapshots snap LEFT JOIN photo.collections col ON col.tenant_id=snap.tenant_id AND col.id=snap.collection_id LEFT JOIN photo.editor_effect_builds effects ON effects.tenant_id=snap.tenant_id AND effects.run_id=c.run_id';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'EDITOR_CULLING_SCOPE_ANCHOR_MISSING';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
