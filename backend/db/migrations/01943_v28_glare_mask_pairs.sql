-- +goose Up
-- Present complete original manual-mask pairs, including their physical pixels.
-- No candidate is assembled from independent target/protection producers.
-- +goose StatementBegin
CREATE FUNCTION photo.list_v28_glare_mask_pairs(p_tenant uuid,p_project uuid,
 p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb,p_after uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE items jsonb:='[]'::jsonb;row record;seen integer:=0;cursor_id uuid;
BEGIN
 IF photo.v28_local_portrait_source_scope(p_tenant,p_project,p_snapshot,p_actor,
  p_session,p_base) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 FOR row IN SELECT target.id target_id,target.sha256 target_sha,
  guard.id guard_id,guard.sha256 guard_sha,
  target_pixel.id target_pixel_id,target_pixel.sha256 target_pixel_sha,
  guard_pixel.id guard_pixel_id,guard_pixel.sha256 guard_pixel_sha
 FROM photo.artifacts target
 JOIN photo.artifacts guard ON guard.tenant_id=target.tenant_id
  AND guard.created_by_task_id=target.created_by_task_id
  AND guard.output_role='mask_guard_ref'
 JOIN photo.artifacts target_pixel ON target_pixel.tenant_id=target.tenant_id
  AND target_pixel.created_by_task_id=target.created_by_task_id
  AND target_pixel.output_role='mask_target'
 JOIN photo.artifacts guard_pixel ON guard_pixel.tenant_id=target.tenant_id
  AND guard_pixel.created_by_task_id=target.created_by_task_id
  AND guard_pixel.output_role='mask_protected'
 JOIN LATERAL photo.v28_manual_pair_origin(p_tenant,p_project,p_snapshot,
  p_actor,p_session,p_base,target.id,target.sha256,guard.id,guard.sha256,
  target_pixel.id,target_pixel.sha256,guard_pixel.id,guard_pixel.sha256) origin ON true
 WHERE target.tenant_id=p_tenant AND target.output_role='mask_target_ref'
  AND (p_after IS NULL OR target.id>p_after)
 ORDER BY target.id LIMIT 101
 LOOP
  seen:=seen+1;
  IF seen>100 THEN EXIT;END IF;
  cursor_id:=row.target_id;
  items:=items||jsonb_build_array(jsonb_build_object(
   'glasses_mask',jsonb_build_object('artifact_id',row.target_id,
    'sha256',row.target_sha),
   'protected',jsonb_build_object('artifact_id',row.guard_id,
    'sha256',row.guard_sha),
   'target_pixel',jsonb_build_object('artifact_id',row.target_pixel_id,
    'sha256',row.target_pixel_sha),
   'protected_pixel',jsonb_build_object('artifact_id',row.guard_pixel_id,
    'sha256',row.guard_pixel_sha)));
 END LOOP;
 RETURN jsonb_build_object('items',items,
  'next_cursor',CASE WHEN seen>100 THEN cursor_id ELSE NULL END);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.list_v28_glare_mask_pairs(uuid,uuid,uuid,uuid,uuid,jsonb,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.list_v28_glare_mask_pairs(uuid,uuid,uuid,uuid,uuid,
 jsonb,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.list_v28_glare_mask_pairs(uuid,uuid,uuid,uuid,
 uuid,jsonb,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
