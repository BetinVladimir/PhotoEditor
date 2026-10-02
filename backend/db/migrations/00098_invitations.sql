-- +goose Up
CREATE UNIQUE INDEX invitations_token_hash_uq ON photo.invitations(token_hash);

-- +goose StatementBegin
CREATE FUNCTION photo.create_invitation(p_user uuid,p_session uuid,p_tenant uuid,p_invitation uuid,p_email text,p_role text,p_token_hash bytea,p_expires timestamptz,p_payload uuid,p_recipient bytea,p_cipher bytea,p_key_id text,p_message_id text,p_event uuid,p_idem_key text,p_request_hash text)
RETURNS TABLE(outcome text,invitation_id uuid,email_normalized text,role text,expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE;i photo.invitations%ROWTYPE;
BEGIN
 IF p_role NOT IN ('admin','editor','viewer') OR p_expires<=clock_timestamp() OR octet_length(p_token_hash)<>32 THEN RAISE EXCEPTION 'INVALID_INVITATION' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT FROM photo.authorize_tenant(p_user,p_session,p_tenant) a WHERE a.role IN ('owner','admin')) THEN RETURN QUERY SELECT 'not_found',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
 VALUES(p_tenant,'user',p_user,'tenant.invite',p_idem_key,p_request_hash,clock_timestamp()+interval '30 days') ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO idem FROM photo.idempotency_records x WHERE x.tenant_id=p_tenant AND x.principal_type='user' AND x.principal_id=p_user AND x.route='tenant.invite' AND x.key=p_idem_key FOR UPDATE;
 IF idem.request_hash<>p_request_hash THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
 IF idem.resource_id IS NOT NULL THEN SELECT * INTO i FROM photo.invitations WHERE tenant_id=p_tenant AND id=idem.resource_id;RETURN QUERY SELECT 'replayed',i.id,i.email_normalized,i.role,i.expires_at;RETURN;END IF;
 INSERT INTO photo.invitations(id,tenant_id,email_normalized,role,token_hash,expires_at,invited_by) VALUES(p_invitation,p_tenant,p_email,p_role,p_token_hash,p_expires,p_user) RETURNING * INTO i;
 INSERT INTO photo.mail_payloads(id,invitation_id,recipient_cipher,payload_cipher,key_id,expires_at,message_id,kind) VALUES(p_payload,i.id,p_recipient,p_cipher,p_key_id,p_expires,p_message_id,'invite');
 INSERT INTO photo.auth_mail_outbox(event_id,payload_id) VALUES(p_event,p_payload);
 UPDATE photo.idempotency_records SET response_status=201,response_body=jsonb_build_object('id',i.id),resource_id=i.id WHERE id=idem.id;
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES(p_tenant,'user',p_user,'invitation.create','invitation',i.id,gen_random_uuid(),jsonb_build_object('role',i.role));
 RETURN QUERY SELECT 'created',i.id,i.email_normalized,i.role,i.expires_at;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_invitation(uuid,uuid,uuid,uuid,text,text,bytea,timestamptz,uuid,bytea,bytea,text,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_invitation(uuid,uuid,uuid,uuid,text,text,bytea,timestamptz,uuid,bytea,bytea,text,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_invitation(uuid,uuid,uuid,uuid,text,text,bytea,timestamptz,uuid,bytea,bytea,text,text,uuid,text,text) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.accept_invitation(p_user uuid,p_session uuid,p_token_hash bytea)
RETURNS TABLE(outcome text,membership_id uuid,tenant_id uuid,role text,status text,revision bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE u photo.users%ROWTYPE;i photo.invitations%ROWTYPE;m photo.memberships%ROWTYPE;
BEGIN
 SELECT x.* INTO u FROM photo.users x JOIN photo.sessions s ON s.user_id=x.id AND s.id=p_session AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() WHERE x.id=p_user AND x.status='active';
 IF NOT FOUND THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,NULL::text,NULL::text,NULL::bigint;RETURN;END IF;
 SELECT * INTO i FROM photo.invitations x WHERE x.token_hash=p_token_hash FOR UPDATE;
 IF NOT FOUND OR i.email_normalized<>u.email_normalized OR i.expires_at<=clock_timestamp() THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,NULL::text,NULL::text,NULL::bigint;RETURN;END IF;
 IF NOT EXISTS(SELECT FROM photo.tenants t WHERE t.id=i.tenant_id AND t.status='active' FOR UPDATE) THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,NULL::text,NULL::text,NULL::bigint;RETURN;END IF;
 IF i.accepted_at IS NOT NULL THEN SELECT * INTO m FROM photo.memberships x WHERE x.tenant_id=i.tenant_id AND x.user_id=p_user;IF NOT FOUND THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,NULL::text,NULL::text,NULL::bigint;ELSE RETURN QUERY SELECT 'accepted',m.id,m.tenant_id,m.role,m.status,m.revision;END IF;RETURN;END IF;
 SELECT * INTO m FROM photo.memberships x WHERE x.tenant_id=i.tenant_id AND x.user_id=p_user FOR UPDATE;
 IF FOUND THEN UPDATE photo.memberships SET status='active' WHERE id=m.id RETURNING * INTO m;ELSE INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES(i.tenant_id,p_user,i.role,'active') RETURNING * INTO m;END IF;
 UPDATE photo.invitations SET accepted_at=clock_timestamp() WHERE id=i.id;
 UPDATE photo.tenants SET revision=revision+1 WHERE id=i.tenant_id AND status='active';
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES(i.tenant_id,'user',p_user,'invitation.accept','invitation',i.id,gen_random_uuid(),'{}');
 RETURN QUERY SELECT 'accepted',m.id,m.tenant_id,m.role,m.status,m.revision;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.accept_invitation(uuid,uuid,bytea) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.accept_invitation(uuid,uuid,bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.accept_invitation(uuid,uuid,bytea) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
