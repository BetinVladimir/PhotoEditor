-- +goose Up
-- Align tenant deletion with the scheduler cancellation state machine.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.request_tenant_deletion(p_user uuid,p_session uuid,p_tenant uuid,p_expected bigint,p_job uuid)
RETURNS TABLE(outcome text,job_id uuid) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tenants%ROWTYPE;existing uuid;
BEGIN
 SELECT x.* INTO t FROM photo.tenants x JOIN photo.memberships m ON m.tenant_id=x.id AND m.user_id=p_user AND m.role='owner' AND m.status='active'
 JOIN photo.sessions s ON s.id=p_session AND s.user_id=p_user AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
 WHERE x.id=p_tenant FOR UPDATE OF x;
 IF NOT FOUND THEN RETURN QUERY SELECT 'not_found',NULL::uuid;RETURN;END IF;
 IF t.status='deleting' THEN
 IF EXISTS(SELECT FROM photo.runs WHERE tenant_id=t.id AND status IN ('draft','queued','running','waiting_review') AND revision>=9007199254740991) THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
  UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1
   WHERE tenant_id=t.id AND status IN ('draft','queued','running','waiting_review');
  SELECT id INTO existing FROM photo.deletion_jobs WHERE tenant_id=t.id AND resource_type='tenant' AND resource_id=t.id ORDER BY created_at LIMIT 1;RETURN QUERY SELECT 'deleting',existing;RETURN;END IF;
 IF t.status<>'active' THEN RETURN QUERY SELECT 'conflict',NULL::uuid;RETURN;END IF;
 IF t.revision<>p_expected THEN RETURN QUERY SELECT 'revision_conflict',NULL::uuid;RETURN;END IF;
 IF EXISTS(SELECT FROM photo.runs WHERE tenant_id=t.id AND status IN ('draft','queued','running','waiting_review') AND revision>=9007199254740991) THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.tenants SET status='deleting',revision=revision+1 WHERE id=t.id;
 UPDATE photo.share_links SET revoked_at=coalesce(revoked_at,clock_timestamp()),policy_version=policy_version+1 WHERE tenant_id=t.id AND revoked_at IS NULL;
 UPDATE photo.share_sessions SET revoked_at=coalesce(revoked_at,clock_timestamp()) WHERE tenant_id=t.id AND revoked_at IS NULL;
 UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1
  WHERE tenant_id=t.id AND status IN ('draft','queued','running','waiting_review');
 INSERT INTO photo.deletion_jobs(id,tenant_id,resource_type,resource_id,requested_by) VALUES(p_job,t.id,'tenant',t.id,p_user);
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES(t.id,'user',p_user,'tenant.delete_requested','tenant',t.id,gen_random_uuid(),jsonb_build_object('job_id',p_job));
 RETURN QUERY SELECT 'queued',p_job;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.request_tenant_deletion(uuid,uuid,uuid,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.request_tenant_deletion(uuid,uuid,uuid,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.request_tenant_deletion(uuid,uuid,uuid,bigint,uuid) TO photo_api;

-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT FROM photo.runs r JOIN photo.tenants t ON t.id=r.tenant_id WHERE t.status='deleting' AND r.status IN ('draft','queued','running','waiting_review') AND r.revision>=9007199254740991 AND EXISTS(SELECT FROM photo.deletion_jobs j WHERE j.tenant_id=r.tenant_id AND j.resource_type='tenant' AND j.resource_id=r.tenant_id AND j.status IN ('queued','running'))) THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 -- Existing queued deletion jobs already represent an authenticated owner's
 -- deletion request. Repair only their nonterminal runs; never terminal output.
 UPDATE photo.runs r SET cancel_requested=true,status='cancelling',revision=revision+1
 WHERE r.status IN ('draft','queued','running','waiting_review')
 AND EXISTS(SELECT FROM photo.tenants t WHERE t.id=r.tenant_id AND t.status='deleting')
 AND EXISTS(SELECT FROM photo.deletion_jobs j WHERE j.tenant_id=r.tenant_id
  AND j.resource_type='tenant' AND j.resource_id=r.tenant_id AND j.status IN ('queued','running'));
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
