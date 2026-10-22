"""Authored migration scope checks; execution is deferred to deep testing."""

def test_report_scope_retains_source_and_privilege_guards(db):
    definition = db.execute("SELECT pg_get_functiondef('photo.v30_interchange_prepared_scope_plain(uuid,uuid,text,uuid,text,text)'::regprocedure)").fetchone()[0]
    assert "'DefectProposeReport'" in definition
    assert "photo.v30_interchange_import_sources" in definition
    assert "photo.v30_interchange_metadata_receipts" in definition
    assert "photo.v30_interchange_import_file" in definition
    assert "interchange:dependency:" in definition
    assert "application/json" in definition
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_gateway', 'photo_portal'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.v30_interchange_prepared_scope_plain(uuid,uuid,text,uuid,text,text)','EXECUTE')", (role,)).fetchone()[0] is False


def test_png_occurrence_scope_is_private_and_restores_label_map(db):
    plain = db.execute("SELECT pg_get_functiondef('photo.v30_interchange_prepared_scope_plain(uuid,uuid,text,uuid,text,text)'::regprocedure)").fetchone()[0]
    assert "'DefectLabelPNG'" in plain
    assert "'DefectLabelMap'" in plain
    assert "'image/png'" in plain
    definition = db.execute("SELECT pg_get_functiondef('photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text)'::regprocedure)").fetchone()[0]
    assert 'defect_png_occurrence' in definition
    assert 'DefectProposeReport' in definition
    assert 'v30_interchange_import_file' in definition
    assert 'actual_bytes IS DISTINCT FROM' in definition
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_gateway', 'photo_portal'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text)','EXECUTE')", (role,)).fetchone()[0]
