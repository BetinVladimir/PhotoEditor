from uuid import uuid4

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
    share = db.execute("INSERT INTO photo.share_links(tenant_id,collection_id,secret_hash,permissions,limits,allowed_presets,created_by) VALUES(%s,%s,%s,%s,'{}',%s,%s) RETURNING id", (tenant, collection, b"s" * 32, Jsonb({"view": True, "download": True, "upload": False}), Jsonb([str(preset)]), user)).fetchone()[0]
    session_hash, csrf_hash = b"x" * 32, b"y" * 32
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

