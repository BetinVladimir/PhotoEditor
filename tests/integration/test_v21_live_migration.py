"""The deployed migration is installed by the ordinary migration fixture."""


def test_v21_live_migration_is_installed_with_api_permissions(db):
    assert db.execute("SELECT to_regclass('photo.v2_document_streams') IS NOT NULL").fetchone()[0]
    assert db.execute("SELECT to_regclass('photo.v2_draft_uploads') IS NOT NULL").fetchone()[0]
    assert db.execute("SELECT to_regprocedure('photo.replace_v2_draft("
                      "uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,jsonb,"
                      "uuid,uuid,text,jsonb)') IS NOT NULL").fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_api',"
                      "'photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text)',"
                      "'EXECUTE')").fetchone()[0]
    assert db.execute("SELECT NOT has_table_privilege('photo_api',"
                      "'photo.v2_draft_uploads','DELETE')").fetchone()[0]
