from uuid import uuid4,uuid5
from hashlib import sha256
import json
from psycopg.types.json import Jsonb
from test_v26_region_source import _region_scope_fixture


def test_region_source_snapshot_freezes_and_replays(db):
    tenant,actor,session,plan,_,_,_,_,_=_region_scope_fixture(db)
    run=uuid4();target=uuid5(run,'region.snapshot')
    descriptor=dict(schema_version=1,snapshot_id=str(target),binding=plan['frozen_recipe']['source'])
    raw=json.dumps(descriptor,sort_keys=True,separators=(',',':'))
    digest=sha256(raw.encode()).hexdigest()
    def freeze(body=raw,sha=digest,auth=session):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_api')
            try:return db.execute('SELECT photo.freeze_v26_region_snapshot(%s,%s,%s,%s,%s,%s,%s)',(tenant,run,actor,auth,Jsonb(plan),body,sha)).fetchone()[0]
            finally:db.execute('RESET ROLE')
    assert freeze() is True
    assert freeze() is True
    assert db.execute('SELECT digest,item_count,expires_at FROM photo.snapshots WHERE id=%s',(target,)).fetchone()==(digest,1,None)
    assert db.execute('SELECT count(*) FROM photo.snapshot_items WHERE snapshot_id=%s',(target,)).fetchone()==(1,)
    assert freeze(body=raw+' ') is False
    assert freeze(auth=uuid4()) is False
    changed=dict(descriptor,snapshot_id=str(uuid4()))
    altered=json.dumps(changed,sort_keys=True,separators=(',',':'))
    assert freeze(body=altered,sha=sha256(altered.encode()).hexdigest()) is False
