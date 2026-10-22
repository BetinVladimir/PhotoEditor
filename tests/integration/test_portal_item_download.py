from uuid import uuid4

import pytest

from psycopg.types.json import Jsonb


def setup_downloadable_item(db):
    digest = "a" * 64
    user, _ = db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)", (f"{uuid4()}@test.example",)).fetchone()
    tenant = db.execute("SELECT id FROM photo.tenants WHERE owner_user_id=%s", (user,)).fetchone()[0]
    project = db.execute("INSERT INTO photo.projects(tenant_id,name,visibility,download_allowed) VALUES(%s,'Portal item','workspace',true) RETURNING id", (tenant,)).fetchone()[0]
    collection = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES(%s,%s,'Published','published') RETURNING id", (tenant, project)).fetchone()[0]
    master = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES(%s,'photo',%s,'master-v1',%s,10,'image/jpeg','working_image') RETURNING id", (tenant, f"masters/{uuid4()}", digest)).fetchone()[0]
    asset = db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id", (tenant, project, master)).fetchone()[0]
    version = db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id", (tenant, asset, master, digest)).fetchone()[0]
    item = db.execute("INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position,visibility,moderation_state) VALUES(%s,%s,%s,%s,1,'visible','accepted') RETURNING id", (tenant, collection, asset, version)).fetchone()[0]
    db.execute("INSERT INTO photo.publications(tenant_id,item_id,asset_version_id,published_by,published_at) VALUES(%s,%s,%s,%s,clock_timestamp())", (tenant, item, version, user))
    preset = db.execute("INSERT INTO photo.export_presets(tenant_id,name,config,config_hash) VALUES(%s,'JPEG',%s,%s) RETURNING id", (tenant, Jsonb({"format": "jpeg"}), "b" * 64)).fetchone()[0]
    exported = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES(%s,'photo',%s,'export-v1',%s,20,'image/jpeg','export') RETURNING id", (tenant, f"exports/{uuid4()}", "c" * 64)).fetchone()[0]
    db.execute("INSERT INTO photo.exports(tenant_id,asset_version_id,preset_hash,artifact_id,status,metadata_policy) VALUES(%s,%s,%s,%s,'ready','{}')", (tenant, version, "b" * 64, exported))
    share = db.execute("INSERT INTO photo.share_links(tenant_id,collection_id,secret_hash,permissions,limits,allowed_presets,created_by) VALUES(%s,%s,%s,%s,'{}',%s,%s) RETURNING id", (tenant, collection, uuid4().bytes + uuid4().bytes, Jsonb({"view": True, "download": True, "upload": False}), Jsonb([str(preset)]), user)).fetchone()[0]
    session_hash, csrf_hash = uuid4().bytes + uuid4().bytes, uuid4().bytes + uuid4().bytes
    session = db.execute("INSERT INTO photo.share_sessions(tenant_id,share_id,collection_id,session_hash,link_version,expires_at,locale,csrf_hash) VALUES(%s,%s,%s,%s,1,clock_timestamp()+interval '1 hour','en',%s) RETURNING id", (tenant, share, collection, session_hash, csrf_hash)).fetchone()[0]
    return tenant, share, session, session_hash, csrf_hash, item, preset, exported


def test_item_intent_redeems_exact_export_once(db):
    tenant, _, session, session_hash, csrf_hash, item, preset, exported = setup_downloadable_item(db)
    intent = uuid4()
    row = db.execute("SELECT outcome,intent_id FROM photo.create_portal_item_download_intent(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp()+interval '60 seconds')", (session_hash, csrf_hash, "one", "d" * 64, item, preset, intent, b"t" * 32)).fetchone()
    assert row == ("created", intent)
    stored = db.execute("SELECT principal_id,artifact_id,intent_kind,item_id,preset_id FROM photo.download_intents WHERE tenant_id=%s AND id=%s", (tenant, intent)).fetchone()
    assert stored == (session, exported, "item", item, preset)
    redeemed = db.execute("SELECT outcome,object_key,object_version,sha256,bytes,media_type,filename FROM photo.redeem_download_intent(%s,'photo')", (b"t" * 32,)).fetchone()
    assert redeemed[0] == "ok" and redeemed[2:] == ("export-v1", "c" * 64, 20, "image/jpeg", f"photo-{str(item)[:8]}.jpg")
    assert db.execute("SELECT outcome FROM photo.redeem_download_intent(%s,'photo')", (b"t" * 32,)).fetchone()[0] == "invalid"


def test_item_intent_is_revoked_by_policy_change(db):
    tenant, share, _, session_hash, csrf_hash, item, preset, _ = setup_downloadable_item(db)
    intent = uuid4()
    assert db.execute("SELECT outcome FROM photo.create_portal_item_download_intent(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp()+interval '60 seconds')", (session_hash, csrf_hash, "two", "e" * 64, item, preset, intent, b"u" * 32)).fetchone()[0] == "created"
    db.execute("UPDATE photo.share_links SET permissions=jsonb_set(permissions,'{download}','false'),policy_version=policy_version+1 WHERE tenant_id=%s AND id=%s", (tenant, share))
    assert db.execute("SELECT outcome FROM photo.redeem_download_intent(%s,'photo')", (b"u" * 32,)).fetchone()[0] == "revoked"



def issue_item_intent(db, setup):
    tenant, share, session, session_hash, csrf_hash, item, preset, artifact = setup
    intent, token_hash = uuid4(), uuid4().bytes + uuid4().bytes
    result = db.execute(
        "SELECT outcome,intent_id FROM photo.create_portal_item_download_intent(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp()+interval '45 seconds')",
        (session_hash, csrf_hash, str(uuid4()), "f" * 64, item, preset, intent, token_hash),
    ).fetchone()
    assert result == ("created", intent)
    return intent, token_hash


def redeem_as_portal(db, token_hash):
    db.execute('SET LOCAL ROLE photo_portal')
    try:
        return db.execute(
            "SELECT outcome,object_key,object_version,sha256,bytes,media_type,filename FROM photo.redeem_download_intent(%s,'photo')",
            (token_hash,),
        ).fetchone()
    finally:
        db.execute('RESET ROLE')


def current_as_portal(db, token_hash, coordinates):
    db.execute('SET LOCAL ROLE photo_portal')
    try:
        return db.execute(
            "SELECT photo.v30_delivery_download_intent_current(%s,'photo',%s,%s,%s,%s,%s)",
            (token_hash, *coordinates),
        ).fetchone()[0]
    finally:
        db.execute('RESET ROLE')


@pytest.mark.parametrize('change', ['intent_expiry', 'session_revoke', 'link_revoke', 'permission', 'object_version'])
def test_item_postsign_authority_rechecks_live_state(db, change):
    setup = setup_downloadable_item(db)
    tenant, share, session, _, _, _, _, artifact = setup
    intent, token_hash = issue_item_intent(db, setup)
    # The final authority check is valid only for an already consumed token.
    coordinates = db.execute(
        'SELECT object_key,object_version,sha256,bytes,media_type FROM photo.artifacts WHERE tenant_id=%s AND id=%s',
        (tenant, artifact),
    ).fetchone()
    assert current_as_portal(db, token_hash, coordinates) is False
    assert redeem_as_portal(db, token_hash)[0] == 'ok'
    assert current_as_portal(db, token_hash, coordinates) is True
    assert redeem_as_portal(db, token_hash)[0] == 'invalid'
    if change == 'intent_expiry':
        db.execute("UPDATE photo.download_intents SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s", (tenant, intent))
    elif change == 'session_revoke':
        db.execute('UPDATE photo.share_sessions SET revoked_at=clock_timestamp() WHERE tenant_id=%s AND id=%s', (tenant, session))
    elif change == 'link_revoke':
        db.execute('UPDATE photo.share_links SET revoked_at=clock_timestamp() WHERE tenant_id=%s AND id=%s', (tenant, share))
    elif change == 'permission':
        db.execute("UPDATE photo.share_links SET permissions=jsonb_set(permissions,'{download}','false'::jsonb) WHERE tenant_id=%s AND id=%s", (tenant, share))
    else:
        # Object identity is immutable; simulate a signer returning the wrong version.
        coordinates = (coordinates[0], 'replacement-v2', *coordinates[2:])
    assert current_as_portal(db, token_hash, coordinates) is False


def test_item_redemption_refuses_expired_intent_without_consuming(db):
    setup = setup_downloadable_item(db)
    tenant = setup[0]
    intent, token_hash = issue_item_intent(db, setup)
    db.execute("UPDATE photo.download_intents SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s", (tenant, intent))
    assert redeem_as_portal(db, token_hash)[0] == 'invalid'
    assert db.execute('SELECT consumed_at FROM photo.download_intents WHERE tenant_id=%s AND id=%s', (tenant, intent)).fetchone() == (None,)


def test_item_intent_refuses_foreign_tenant_item_and_preset(db):
    setup = setup_downloadable_item(db)
    tenant, _, _, session_hash, csrf_hash, item, preset, _ = setup
    foreign = setup_downloadable_item(db)
    assert foreign[0] != tenant
    for selected_item, selected_preset in [(foreign[5], preset), (item, foreign[6])]:
        intent = uuid4()
        result = db.execute(
            "SELECT outcome FROM photo.create_portal_item_download_intent(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp()+interval '45 seconds')",
            (session_hash, csrf_hash, str(uuid4()), 'f' * 64, selected_item, selected_preset, intent, uuid4().bytes + uuid4().bytes),
        ).fetchone()[0]
        assert result in ('revoked', 'preset_invalid')
        assert db.execute('SELECT count(*) FROM photo.download_intents WHERE id=%s', (intent,)).fetchone() == (0,)

@pytest.mark.parametrize("defect", ["consumed", "expired"])
def test_item_download_intent_replay_does_not_return_dead_bearer(db, defect):
    setup = setup_downloadable_item(db)
    tenant, _, _, session_hash, csrf_hash, item, preset, _ = setup
    intent, token = uuid4(), uuid4().bytes + uuid4().bytes
    key = str(uuid4())
    def issue():
        return db.execute("SELECT outcome,intent_id FROM photo.create_portal_item_download_intent(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp()+interval '45 seconds')",
            (session_hash, csrf_hash, key, "f" * 64, item, preset, intent, token)).fetchone()
    assert issue() == ("created", intent)
    if defect == "consumed":
        db.execute("UPDATE photo.download_intents SET consumed_at=clock_timestamp() WHERE tenant_id=%s AND id=%s", (tenant,intent))
    else:
        db.execute("UPDATE photo.download_intents SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s", (tenant,intent))
    assert issue() == ("not_ready", None)
