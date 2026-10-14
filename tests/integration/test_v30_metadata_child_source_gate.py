"""Frozen child metadata source gate tests; authored, NOT RUN."""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_child_source_gate_private(db,role):
    signature='photo.v30_metadata_child_source_ready(uuid,uuid,jsonb,jsonb,text[])'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(False,)


@pytest.mark.parametrize('config',[None,{}, {'queue_class':'batch'}, {'v30_metadata_approved_fields':['gps']}])
def test_missing_frozen_metadata_fields_refused(db,config):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        assert db.execute('SELECT photo.v30_metadata_child_source_ready(%s,%s,%s,%s,%s)',
            (uuid4(),uuid4(),Jsonb(config),Jsonb({}),[])).fetchone()==(False,)
