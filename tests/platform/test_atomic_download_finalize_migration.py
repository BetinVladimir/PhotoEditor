from pathlib import Path


SQL=(Path(__file__).resolve().parents[2]/'backend/db/migrations/00170_atomic_download_archive_finalize.sql').read_text()


def test_download_finalizer_validates_all_volumes_before_first_update():
    validation=SQL.index('-- Phase one')
    publication=SQL.index('-- Phase two')
    first_update=SQL.index("UPDATE photo.download_volumes SET artifact_id")
    assert validation<publication<first_update
    assert SQL.count("FOR v IN SELECT volume.id,volume.volume_no,p.id task_id") == 2
    assert "IF archive.id IS NULL OR manifest.id IS NULL" in SQL


def test_atomic_finalizer_is_forward_only_and_role_scoped():
    assert SQL.count('-- +goose StatementBegin')==SQL.count('-- +goose StatementEnd')==2
    assert "GRANT EXECUTE ON FUNCTION photo.finalize_download_archives(uuid,uuid) TO photo_scheduler" in SQL
    assert "RAISE EXCEPTION 'Forward-only migration'" in SQL
