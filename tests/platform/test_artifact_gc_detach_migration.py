from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00101_artifact_gc_detach.sql').read_text()
def test_detach_preserves_permanent_and_active_references():
    for value in ('photo.assets','photo.asset_versions','photo.style_versions','photo.snapshots','photo.tasks','photo.reviews',"j.expires_at IS NULL OR j.expires_at>clock_timestamp()"):
        assert value in SQL
def test_detach_expires_only_lifecycle_owned_references_before_gc():
    for value in ("photo.derivatives SET artifact_id=NULL,status='queued',task_id=NULL","photo.exports SET artifact_id=NULL,status='queued',task_id=NULL","photo.download_jobs j SET status='expired'","DELETE FROM photo.download_intents","photo.download_job_items r SET artifact_id=NULL","photo.download_volumes r SET artifact_id=NULL,manifest_artifact_id=NULL,status='expired'","photo.artifact_has_references(a.tenant_id,a.id)","state='deleting',gc_token=token"):
        assert value in SQL
