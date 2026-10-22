-- +goose Up
CREATE TABLE photo.apply_culling_reviews (
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),run_id uuid NOT NULL,review_id uuid NOT NULL,
 snapshot_id uuid NOT NULL,snapshot_digest text NOT NULL CHECK(snapshot_digest~'^[0-9a-f]{64}$'),
 projection jsonb NOT NULL CHECK(jsonb_typeof(projection)='object' AND octet_length(projection::text)<=8388608),
 PRIMARY KEY(tenant_id,run_id),UNIQUE(tenant_id,review_id),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,review_id) REFERENCES photo.reviews(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,snapshot_id) REFERENCES photo.snapshots(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE photo.apply_culling_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.apply_culling_reviews FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.apply_culling_reviews TO photo_api USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.apply_culling_reviews TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.apply_culling_reviews FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT SELECT ON photo.apply_culling_reviews TO photo_api;
GRANT SELECT,INSERT,DELETE ON photo.apply_culling_reviews TO photo_fn_owner;
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends) VALUES
 ('collection.features','1','cpu','cpu.collection_features',NULL,300,'["cpu"]'),
 ('collection.features_pack','1','cpu','cpu.collection_features_pack',NULL,300,'["cpu"]');
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('collection.features','1','scene_features','["application/json"]',1048576,1),
 ('collection.features_pack','1','scene_feature_pack','["application/json"]',1048576,1);
UPDATE photo.operation_registry SET timeout_seconds=7200 WHERE operation='collection.group' AND version='1';
UPDATE photo.operation_output_policies SET max_bytes=8388608 WHERE (operation,version,role) IN
 (('collection.group','1','scene_groups'),('collection.cull','1','cull_recommendations'));
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;constraint_name text;
BEGIN
 -- Preserve every installed route while adding the selection decision.
 SELECT conname,pg_get_constraintdef(oid) INTO constraint_name,definition FROM pg_constraint
 WHERE conrelid='photo.reviews'::regclass AND contype='c'
 AND conname='reviews_decision_route_domain';
 IF constraint_name IS NULL OR position('selection' IN definition)>0 THEN RAISE EXCEPTION 'CULL_ROUTE_SOURCE_SHAPE';END IF;
 EXECUTE format('ALTER TABLE photo.reviews DROP CONSTRAINT %I',constraint_name);
 EXECUTE format('ALTER TABLE photo.reviews ADD CONSTRAINT %I CHECK (decision_route = %L OR (%s))',constraint_name,'selection',substring(definition from 8 for length(definition)-8));
 definition:=pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure);needle:=' SET CONSTRAINTS ALL DEFERRED;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'CULL_PURGE_SOURCE_SHAPE';END IF;
 EXECUTE replace(definition,needle,needle||' DELETE FROM photo.apply_culling_reviews WHERE tenant_id=j.tenant_id;');
 definition:=pg_get_functiondef('photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb)'::regprocedure);needle:='allowed constant text[]:=ARRAY[';
 IF position(needle IN definition)=0 THEN RAISE EXCEPTION 'CULL_PLAN_SOURCE_SHAPE';END IF;
 EXECUTE replace(definition,needle,needle||'''features.scene'',''collection.features'',''collection.features_pack'',''collection.group'',''collection.cull'',');
 ALTER FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
 REVOKE ALL ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
 GRANT EXECUTE ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
 definition:=pg_get_functiondef('photo.claim_apply_build_versioned()'::regprocedure);
 needle:='OR (previous_stage.phase=''defects_review''';
 IF position(needle IN definition)=0 THEN RAISE EXCEPTION 'CULL_CLAIM_PREVIOUS_SHAPE';END IF;
 definition:=replace(definition,needle,'OR (previous_stage.phase=''cull_review'' AND EXISTS(SELECT FROM photo.apply_culling_reviews cr JOIN photo.reviews cv ON cv.tenant_id=cr.tenant_id AND cv.id=cr.review_id WHERE cr.tenant_id=b.tenant_id AND cr.run_id=b.run_id AND cv.state=''decided'' AND cv.decision_route=''selection'')) '||needle);
 needle:='AND (current_stage.phase=''defects_review''';
 IF position(needle IN definition)=0 THEN RAISE EXCEPTION 'CULL_CLAIM_CURRENT_SHAPE';END IF;
 definition:=replace(definition,needle,'AND ((current_stage.phase=''cull_review'' AND (NOT EXISTS(SELECT FROM photo.apply_culling_reviews cr WHERE cr.tenant_id=b.tenant_id AND cr.run_id=b.run_id) OR EXISTS(SELECT FROM photo.apply_culling_reviews cr JOIN photo.reviews cv ON cv.tenant_id=cr.tenant_id AND cv.id=cr.review_id WHERE cr.tenant_id=b.tenant_id AND cr.run_id=b.run_id AND cv.state=''decided'' AND cv.decision_route=''selection''))) OR current_stage.phase=''defects_review''');
 EXECUTE definition;
 ALTER FUNCTION photo.claim_apply_build_versioned() OWNER TO photo_fn_owner;
 REVOKE ALL ON FUNCTION photo.claim_apply_build_versioned() FROM PUBLIC;
 GRANT EXECUTE ON FUNCTION photo.claim_apply_build_versioned() TO photo_scheduler;
 -- The outer atomic-rejection wrapper from02441 is retained unchanged.
 definition:=pg_get_functiondef('photo.adopt_apply_results_before_atomic_rejection(uuid,uuid,uuid)'::regprocedure);
 needle:=' FOR n IN SELECT * FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND phase=source_phase ORDER BY ordinal LOOP';
 IF position(needle IN definition)=0 THEN RAISE EXCEPTION 'CULL_ADOPT_SOURCE_SHAPE';END IF;
 EXECUTE replace(definition,needle,
 ' IF EXISTS(SELECT FROM photo.apply_culling_reviews cr JOIN photo.reviews cv ON cv.tenant_id=cr.tenant_id AND cv.id=cr.review_id WHERE cr.tenant_id=p_tenant AND cr.run_id=p_run AND (cv.state IS DISTINCT FROM ''decided'' OR cv.decision_route IS DISTINCT FROM ''selection'')) THEN RETURN false;END IF;'||needle||
 ' IF EXISTS(SELECT FROM photo.apply_culling_reviews cr WHERE cr.tenant_id=p_tenant AND cr.run_id=p_run) AND NOT EXISTS(SELECT FROM photo.apply_culling_reviews cr JOIN photo.reviews cv ON cv.tenant_id=cr.tenant_id AND cv.id=cr.review_id WHERE cr.tenant_id=p_tenant AND cr.run_id=p_run AND cv.selection->''selected_item_ids'' @> jsonb_build_array(n.source_item_id::text)) THEN skipped:=skipped+1;CONTINUE;END IF;');
 ALTER FUNCTION photo.adopt_apply_results_before_atomic_rejection(uuid,uuid,uuid) OWNER TO photo_fn_owner;
 REVOKE ALL ON FUNCTION photo.adopt_apply_results_before_atomic_rejection(uuid,uuid,uuid) FROM PUBLIC,photo_scheduler;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.register_apply_culling_review(p_tenant uuid,p_run uuid,p_token uuid,p_projection jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;s photo.snapshots%ROWTYPE;rank_output record;group_output record;entry jsonb;new_id uuid;existing photo.apply_culling_reviews%ROWTYPE;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF b.run_id IS NULL OR r.id IS NULL OR r.kind<>'apply' OR b.graph_version<>4 OR b.phase<>'cull_review' OR b.lease_token IS DISTINCT FROM p_token OR (b.lease_until IS NULL OR b.lease_until<clock_timestamp()) OR r.cancel_requested OR r.status NOT IN('running','waiting_review') THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id;
 IF NOT EXISTS(SELECT FROM photo.apply_v4_stages st WHERE st.tenant_id=p_tenant AND st.run_id=p_run AND st.stage_index=b.stage_index AND st.phase='cull_review' AND st.cardinality='review') OR NOT EXISTS(SELECT FROM photo.apply_v4_stages st WHERE st.tenant_id=p_tenant AND st.run_id=p_run AND st.stage_index=b.stage_index-1 AND st.phase='cull_rank' AND st.operation='collection.cull' AND st.cardinality='aggregate') THEN RETURN false;END IF;
 IF s.id IS NULL OR s.digest IS DISTINCT FROM r.config_snapshot->>'input_snapshot_digest' THEN RETURN false;END IF;
 IF p_projection IS NULL OR NOT (p_projection ?& ARRAY['groups','proposal','items','never_delete']) OR jsonb_typeof(p_projection->'groups') IS DISTINCT FROM 'object' OR jsonb_typeof(p_projection->'proposal') IS DISTINCT FROM 'object' OR (p_projection-'groups'-'proposal'-'items'-'never_delete')<>'{}' OR p_projection->'never_delete' IS DISTINCT FROM 'true'::jsonb OR jsonb_typeof(p_projection->'items') IS DISTINCT FROM 'array' OR octet_length(p_projection::text)>8388608 THEN RETURN false;END IF;
 IF jsonb_array_length(p_projection->'items')<>b.item_count THEN RETURN false;END IF;
 SELECT * INTO rank_output FROM photo.apply_v4_stage_outputs(p_tenant,p_run,p_token,'collection.cull','cull_recommendations');
 SELECT * INTO group_output FROM photo.apply_v4_stage_outputs(p_tenant,p_run,p_token,'collection.group','scene_groups');
 IF rank_output.artifact_id IS NULL OR group_output.artifact_id IS NULL OR (SELECT count(*) FROM photo.apply_v4_stage_outputs(p_tenant,p_run,p_token,'collection.cull','cull_recommendations'))<>1 OR (SELECT count(*) FROM photo.apply_v4_stage_outputs(p_tenant,p_run,p_token,'collection.group','scene_groups'))<>1
 OR p_projection->'proposal' IS DISTINCT FROM jsonb_build_object('artifact_id',rank_output.artifact_id,'sha256',rank_output.sha256) OR p_projection->'groups' IS DISTINCT FROM jsonb_build_object('artifact_id',group_output.artifact_id,'sha256',group_output.sha256)
 OR NOT EXISTS(SELECT FROM photo.apply_nodes n WHERE n.tenant_id=p_tenant AND n.id=rank_output.task_id AND n.params->'groups'=p_projection->'groups') THEN RETURN false;END IF;
 IF (SELECT count(DISTINCT value->>'item_id') FROM jsonb_array_elements(p_projection->'items'))<>b.item_count THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_projection->'items') LOOP
  IF jsonb_typeof(entry) IS DISTINCT FROM 'object' OR NOT (entry ?& ARRAY['item_id','asset_id','asset_version_id','scene_id','exact_group_id','burst_group_id','rank','score','recommended','manual','eye_review','eye_state']) OR jsonb_typeof(entry->'recommended') IS DISTINCT FROM 'boolean' OR jsonb_typeof(entry->'manual') IS DISTINCT FROM 'boolean' OR jsonb_typeof(entry->'rank') IS DISTINCT FROM 'number' OR jsonb_typeof(entry->'score') IS DISTINCT FROM 'number' OR (entry->>'rank') !~ '^[1-9][0-9]{0,3}$' OR jsonb_typeof(entry->'scene_id') IS DISTINCT FROM 'string' OR jsonb_typeof(entry->'exact_group_id') IS DISTINCT FROM 'string' OR jsonb_typeof(entry->'burst_group_id') IS DISTINCT FROM 'string' OR length(entry->>'scene_id') NOT BETWEEN 1 AND 80 OR length(entry->>'exact_group_id') NOT BETWEEN 1 AND 80 OR length(entry->>'burst_group_id') NOT BETWEEN 1 AND 80 OR (entry-'item_id'-'asset_id'-'asset_version_id'-'scene_id'-'exact_group_id'-'burst_group_id'-'rank'-'score'-'recommended'-'manual'-'eye_review'-'eye_state')<>'{}' OR entry->>'eye_state' IS DISTINCT FROM 'unknown' OR entry->'eye_review' IS DISTINCT FROM 'true'::jsonb THEN RETURN false;END IF;
  IF (entry->>'rank')::int NOT BETWEEN 1 AND b.item_count OR (entry->>'score')::numeric NOT BETWEEN 0 AND 1 OR NOT EXISTS(SELECT FROM photo.snapshot_items i JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256 WHERE i.tenant_id=p_tenant AND i.snapshot_id=s.id AND i.item_id=(entry->>'item_id')::uuid AND v.asset_id=(entry->>'asset_id')::uuid AND v.id=(entry->>'asset_version_id')::uuid) THEN RETURN false;END IF;
 END LOOP;
 SELECT * INTO existing FROM photo.apply_culling_reviews WHERE tenant_id=p_tenant AND run_id=p_run;
 IF FOUND THEN RETURN existing.snapshot_id=s.id AND existing.snapshot_digest=s.digest AND existing.projection=p_projection;END IF;
 new_id:=gen_random_uuid();
 INSERT INTO photo.reviews(tenant_id,id,run_id,task_id,candidate_artifact_id,base_artifact_id,candidate_hash,kind,reasons)
 VALUES(p_tenant,new_id,p_run,rank_output.task_id,rank_output.artifact_id,NULL,rank_output.sha256,'culling','["photographer_selection_required","unknown_eye_state"]');
 INSERT INTO photo.apply_culling_reviews VALUES(p_tenant,p_run,new_id,s.id,s.digest,p_projection);
 UPDATE photo.apply_builds SET lease_token=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.runs SET status='waiting_review',graph_sealed=false,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR check_violation OR unique_violation THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_apply_culling_review(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_apply_culling_review(uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_apply_culling_review(uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.process_apply_culling_review(p_tenant uuid,p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;v photo.reviews%ROWTYPE;next_stage photo.apply_v4_stages%ROWTYPE;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF b.run_id IS NULL OR r.id IS NULL OR r.kind<>'apply' OR b.graph_version<>4 OR b.phase<>'cull_review' OR b.lease_token IS DISTINCT FROM p_token OR (b.lease_until IS NULL OR b.lease_until<clock_timestamp()) OR r.cancel_requested OR r.status NOT IN('running','waiting_review') THEN RETURN false;END IF;
 SELECT rv.* INTO v FROM photo.apply_culling_reviews cr JOIN photo.reviews rv ON rv.tenant_id=cr.tenant_id AND rv.id=cr.review_id WHERE cr.tenant_id=p_tenant AND cr.run_id=p_run AND cr.snapshot_id=r.input_snapshot_id AND cr.snapshot_digest=r.config_snapshot->>'input_snapshot_digest' FOR UPDATE OF rv;
 IF v.id IS NULL OR v.state IS DISTINCT FROM 'decided' OR v.decision IS DISTINCT FROM 'accept' OR v.decision_route IS DISTINCT FROM 'selection' OR jsonb_typeof(v.selection) IS DISTINCT FROM 'object' OR NOT (v.selection ?& ARRAY['selected_item_ids','confirmed_eyes_item_ids','never_delete']) OR (v.selection-'selected_item_ids'-'confirmed_eyes_item_ids'-'never_delete')<>'{}' OR v.selection->'never_delete' IS DISTINCT FROM 'true'::jsonb OR jsonb_typeof(v.selection->'selected_item_ids') IS DISTINCT FROM 'array' OR jsonb_typeof(v.selection->'confirmed_eyes_item_ids') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(v.selection->'selected_item_ids') NOT BETWEEN 1 AND b.item_count THEN RETURN false;END IF;
 SELECT * INTO next_stage FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index+1;
 IF next_stage.phase IS NULL OR next_stage.cardinality<>'item' THEN RETURN false;END IF;
 UPDATE photo.apply_builds SET stage_index=stage_index+1,phase=next_stage.phase,lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.runs SET status='running',graph_sealed=true,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.process_apply_culling_review(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.process_apply_culling_review(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.process_apply_culling_review(uuid,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
