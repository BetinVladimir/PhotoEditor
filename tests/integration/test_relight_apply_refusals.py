"""Relational Apply refusal scenarios; authored, not executed.

This fixture has no successful adoption/decision authority. Its purpose here is
strictly to ensure a pending, fabricated or foreign review cannot create a
version. It does not prove successful materialization or GPU quality.
"""
from uuid import uuid4
import pytest
from relight_viewed_pair_fixture import relight_viewed_pair_fixture


@pytest.mark.parametrize('case', ['pending', 'foreign_review', 'foreign_actor', 'foreign_tenant'])
def test_relight_apply_refuses_without_decided_authority(db, case):
    fixture = relight_viewed_pair_fixture(db)
    tenant, review, actor = fixture['tenant'], fixture['review'], fixture['actor']
    if case == 'foreign_review':
        review = uuid4()
    elif case == 'foreign_actor':
        actor = uuid4()
    elif case == 'foreign_tenant':
        tenant = uuid4()
    before = db.execute('SELECT count(*) FROM photo.asset_versions WHERE tenant_id=%s',
                        (fixture['tenant'],)).fetchone()[0]
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(fixture['tenant']),))
        db.execute('SET LOCAL ROLE photo_api')
        result = db.execute('SELECT photo.apply_v28_relight_version(%s,%s,%s,%s,%s,%s,%s)',
            (tenant, review, actor, fixture['session'], 'a' * 64, '{}', '{}')).fetchone()[0]
        assert result is None
    assert db.execute('SELECT count(*) FROM photo.asset_versions WHERE tenant_id=%s',
                      (fixture['tenant'],)).fetchone()[0] == before
