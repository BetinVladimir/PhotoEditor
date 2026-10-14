"""Bounded scheduler metadata discovery ACL; authored, NOT RUN."""
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_next_metadata_barrier_scheduler_only(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.claim_next_v30_metadata_barrier()','EXECUTE')",(role,)).fetchone()==(role=='photo_scheduler',)
