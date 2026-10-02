-- +goose Up
-- Entire user-mask set proof, including protected masks and nested expressions.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_original_user_mask_set_scope(p_tenant uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_session uuid,p_params jsonb,p_width bigint,p_height bigint) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ref jsonb; binding jsonb; n integer:=0;
BEGIN
 IF p_width IS NULL OR p_height IS NULL OR p_width NOT BETWEEN 1 AND 200000000 THEN RETURN false;END IF;
 IF p_height<1 OR p_height>200000000/p_width
  OR NOT photo.v25_layer_original_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_params) THEN RETURN false;END IF;
 FOR ref IN WITH RECURSIVE nodes(node) AS (
  SELECT p_params->'mask'
  UNION ALL SELECT c.child FROM nodes x CROSS JOIN LATERAL
   (VALUES(x.node->'left'),(x.node->'right'),(x.node->'operand')) c(child)
   WHERE c.child IS NOT NULL)
  SELECT DISTINCT node->'mask' FROM nodes WHERE node->>'op'='ref'
  UNION SELECT p_params->'protected_mask' WHERE p_params->'protected_mask'<>'null'::jsonb
 LOOP
  n:=n+1;
  SELECT u.user_mask_binding INTO binding FROM photo.v2_draft_uploads u
   WHERE u.tenant_id=p_tenant AND u.artifact_id=(ref->>'artifact_id')::uuid AND u.sha256=ref->>'sha256';
  IF binding IS NULL OR NOT photo.v25_layer_ref_shape(binding->'mask'->'artifact') THEN RETURN false;END IF;
  IF NOT photo.v25_layer_original_user_mask_pixels_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_params,
   (ref->>'artifact_id')::uuid,ref->>'sha256',(binding->'mask'->'artifact'->>'artifact_id')::uuid,
   binding->'mask'->'artifact'->>'sha256',p_width,p_height) THEN RETURN false;END IF;
 END LOOP;
 RETURN n>0;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_original_user_mask_set_scope(uuid,uuid,uuid,uuid,uuid,jsonb,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_original_user_mask_set_scope(uuid,uuid,uuid,uuid,uuid,jsonb,bigint,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_original_user_mask_set_scope(uuid,uuid,uuid,uuid,uuid,jsonb,bigint,bigint) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
