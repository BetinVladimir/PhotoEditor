-- +goose Up
-- Patch exact installed bodies, preserving their existing ACL and qualifications.
-- +goose StatementBegin
DO $$
DECLARE target oid; source text; definition text; patched text;
 old_lock constant text := ' IF FOUND THEN'||chr(10)||'  IF old.worker_id';
 quota_gate constant text := ' IF v2 AND (NOT FOUND OR q.bytes<p_bytes) THEN';
 complete_gate constant text := ' IF NOT FOUND OR u.attempt<>p_attempt';
 begin_guard constant text := $guard$
 -- 02536: receipt/quota waits cannot extend output authority.
 IF NOT EXISTS(SELECT FROM photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate)) THEN
  RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN;
 END IF;
$guard$;
 complete_guard constant text := $guard$
 -- 02536: reauthorize after output upload lock, before replay or publication.
 IF NOT EXISTS(SELECT FROM photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate)) THEN
  RETURN QUERY SELECT 'stale',NULL::uuid,NULL::text; RETURN;
 END IF;
$guard$;
BEGIN
 target:=to_regprocedure('photo.begin_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,text)');
 SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
 WHERE oid=target AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner';
 IF source IS NULL OR strpos(source,'-- 02536:')>0 OR strpos(source,old_lock)=0 OR strpos(source,quota_gate)=0
 OR strpos(source,' IF FOUND AND q.bytes>=p_bytes THEN')=0 THEN RAISE EXCEPTION 'BEGIN_OUTPUT_FENCE_SOURCE_SHAPE';END IF;
 -- The guard uses SQL; use explicit row identity, never ambient FOUND afterward.
 patched:=replace(source,old_lock,' IF old.id IS NOT NULL THEN'||begin_guard||'  IF old.worker_id');
 patched:=replace(patched,quota_gate,begin_guard||$quota$
 IF q.id IS NOT NULL AND (q.state<>'reserved' OR q.expires_at<=clock_timestamp()) THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN;
 END IF;
 IF v2 AND (q.id IS NULL OR q.bytes<p_bytes) THEN$quota$);
 patched:=replace(patched,' IF FOUND AND q.bytes>=p_bytes THEN',' IF q.id IS NOT NULL AND q.bytes>=p_bytes THEN');
 EXECUTE replace(definition,source,patched);
 target:=to_regprocedure('photo.complete_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text)');
 SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
 WHERE oid=target AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner';
 IF source IS NULL OR strpos(source,'-- 02536:')>0 OR strpos(source,complete_gate)=0
 OR strpos(source,' INSERT INTO photo.artifacts(')=0 THEN RAISE EXCEPTION 'COMPLETE_OUTPUT_FENCE_SOURCE_SHAPE';END IF;
 patched:=replace(source,complete_gate,complete_guard||' IF u.id IS NULL OR u.attempt<>p_attempt');
 EXECUTE replace(definition,source,patched);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
