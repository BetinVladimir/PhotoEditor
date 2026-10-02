-- Add deterministic composition structure extraction and admit the complete Apply v4 composition chain.
-- +goose Up
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends)
 VALUES('composition.structure','1','cpu','cpu.composition',NULL,180,'["cpu"]');
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
 VALUES('composition.structure','1','image_structure','["application/json"]',1048576,1);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.register_apply_v4_plan(p_tenant uuid,p_run uuid,p_token uuid,p_hash text,p_plan jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;entry jsonb;i int:=0;old_hash text;
 allowed constant text[]:=ARRAY['mask.person','color.stats','style.resolve','image.denoise','depth.predict','pose.detect','geometry.body','mask.transform','image.color','series.measure','series.balance','image.local','face.detect','face.landmarks','face.parse','mask.face_skin','portrait.beauty','portrait.detail','defects.propose','defects.repair','background.compose','background.blur','image.inpaint','image.generate','composition.structure','composition.plan','geometry.crop','image.upscale','quality.check'];
BEGIN
 IF p_hash !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_plan)<>'array' OR jsonb_array_length(p_plan) NOT BETWEEN 4 AND 64 THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.graph_version<>4 OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() THEN RETURN false;END IF;
 SELECT min(plan_hash) INTO old_hash FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run;
 IF old_hash IS NOT NULL THEN RETURN old_hash=p_hash
  AND NOT EXISTS(SELECT FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND plan_hash<>p_hash)
  AND (SELECT count(*) FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run)=jsonb_array_length(p_plan)
  AND NOT EXISTS(SELECT FROM jsonb_array_elements(p_plan) WITH ORDINALITY x(value,ord)
   LEFT JOIN photo.apply_v4_stages s ON s.tenant_id=p_tenant AND s.run_id=p_run AND s.stage_index=x.ord-1
   WHERE s.stage_index IS NULL OR s.phase<>x.value->>'phase' OR coalesce(s.operation,'')<>coalesce(x.value->>'operation','')
    OR coalesce(s.output_role,'')<>coalesce(x.value->>'output_role','') OR s.cardinality<>x.value->>'cardinality');END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_plan) LOOP
  IF (entry-'phase'-'operation'-'output_role'-'cardinality')<>'{}' OR entry->>'phase' !~ '^[a-z][a-z0-9_]{0,63}$' OR entry->>'cardinality' NOT IN('item','aggregate','review') THEN RETURN false;END IF;
  IF entry->>'cardinality'='review' THEN IF coalesce(entry->>'operation','')<>'' OR coalesce(entry->>'output_role','')<>'' THEN RETURN false;END IF;
  ELSIF NOT ((entry->>'operation')=ANY(allowed)) OR NOT EXISTS(SELECT FROM photo.operation_registry o WHERE o.operation=entry->>'operation' AND o.version='1') OR coalesce(entry->>'output_role','')='' THEN RETURN false;END IF;
  INSERT INTO photo.apply_v4_stages(tenant_id,run_id,stage_index,phase,operation,output_role,cardinality,plan_hash)
  VALUES(p_tenant,p_run,i,entry->>'phase',NULLIF(entry->>'operation',''),NULLIF(entry->>'output_role',''),entry->>'cardinality',p_hash);i:=i+1;
 END LOOP;
 IF NOT EXISTS(SELECT FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=0 AND phase IN('mask','resolve'))
 OR NOT EXISTS(SELECT FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=i-1 AND phase='quality') THEN RAISE EXCEPTION 'INVALID_APPLY_V4_BOUNDARIES' USING ERRCODE='23514';END IF;
 UPDATE photo.apply_builds SET stage_index=0,phase=(p_plan->0->>'phase'),updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run AND graph_version=4;
 RETURN true;
EXCEPTION WHEN unique_violation OR check_violation THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;


-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
