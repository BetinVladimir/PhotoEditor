-- +goose Up
-- Preserve function identity/ACL while checking expiring lease authority after
-- all descriptor/snapshot scope queries, not only before them.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;signature text;
BEGIN
 signature:='photo.v30_export_snapshot_authority(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)';
 definition:=pg_get_functiondef(signature::regprocedure);
 needle:=' RETURN reference;';
 IF (length(definition)-length(replace(definition,needle,'')))<>length(needle)
 OR strpos(definition,'SECURITY DEFINER')=0 OR strpos(definition,'v30_interchange_export_ready')=0 THEN
  RAISE EXCEPTION 'V30_EXPORT_FINAL_SNAPSHOT_BASELINE_MISMATCH';
 END IF;
 replacement:=$replacement$ -- Final authority recheck follows every snapshot scope read.
 context:=photo.v30_interchange_export_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest);
 IF context IS NULL THEN RETURN NULL;END IF;
 RETURN reference;$replacement$;
 EXECUTE replace(definition,needle,replacement);

 signature:='photo.v30_export_descriptor_authority(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,jsonb)';
 definition:=pg_get_functiondef(signature::regprocedure);
 needle:=$needle$ IF kind='DescriptorShard' THEN RETURN ref=descriptor AND frame='null'::jsonb AND p_evidence->'frame_ordinal'='null'::jsonb;END IF;
 IF frame IS NULL OR frame='null'::jsonb THEN RETURN false;END IF;
 IF kind='WorkingImage' THEN$needle$;
 IF (length(definition)-length(replace(definition,needle,'')))<>length(needle)
 OR strpos(definition,'SECURITY DEFINER')=0
 OR strpos(definition,'root_raw text;')=0
 OR strpos(definition,' RETURN ref=frame->''source'' AND photo.v26_render_frame_source_scope')=0
 OR strpos(definition,' RETURN photo.v26_render_frame_faces_scope')=0
 OR strpos(definition,' RETURN photo.v26_render_frame_array_scope')=0 THEN
  RAISE EXCEPTION 'V30_EXPORT_FINAL_DESCRIPTOR_BASELINE_MISMATCH';
 END IF;
 definition:=replace(definition,'root_raw text;','root_raw text;allowed boolean:=false;');
 replacement:=$replacement$ IF kind='DescriptorShard' THEN
 allowed:=ref=descriptor AND frame='null'::jsonb AND p_evidence->'frame_ordinal'='null'::jsonb;
 ELSIF frame IS NULL OR frame='null'::jsonb THEN RETURN false;
 ELSIF kind='WorkingImage' THEN$replacement$;
 definition:=replace(definition,needle,replacement);
 definition:=replace(definition,' RETURN ref=frame->''source'' AND photo.v26_render_frame_source_scope',' allowed:=ref=frame->''source'' AND photo.v26_render_frame_source_scope');
 definition:=replace(definition,' RETURN photo.v26_render_frame_faces_scope',' allowed:=photo.v26_render_frame_faces_scope');
 definition:=replace(definition,' RETURN photo.v26_render_frame_array_scope',' allowed:=photo.v26_render_frame_array_scope');
 needle:=$needle$ END IF;
 RETURN false;
EXCEPTION WHEN$needle$;
 IF (length(definition)-length(replace(definition,needle,'')))<>length(needle) THEN
  RAISE EXCEPTION 'V30_EXPORT_FINAL_DESCRIPTOR_RETURN_MISMATCH';
 END IF;
 replacement:=$replacement$ END IF;
 IF allowed IS DISTINCT FROM true THEN RETURN false;END IF;
 -- Final authority recheck follows every descriptor scope read.
 context:=photo.v30_interchange_export_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest);
 RETURN context IS NOT NULL;
EXCEPTION WHEN$replacement$;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
