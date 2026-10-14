"""Scheduler-only normalized metadata source read; authored, NOT RUN."""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_published_source_read_scheduler_only(db,role):
    signature='photo.read_v30_metadata_source(uuid,uuid,jsonb,jsonb,text,text[])'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_scheduler',)


def test_missing_publication_returns_no_source(db):
    tenant=uuid4()
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        assert db.execute('SELECT * FROM photo.read_v30_metadata_source(%s,%s,%s,%s,%s,%s)',
            (tenant,uuid4(),Jsonb({}),Jsonb({'artifact_id':str(uuid4()),'sha256':'a'*64}),'copyright',[])).fetchall()==[]
