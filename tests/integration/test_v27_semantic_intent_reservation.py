"""Reservation/fence/quota metadata checks; no object publication is implied."""
import json
from uuid import uuid4
from layer_original_fixture import seed_original_composition


def test_semantic_intent_old_version_reservation_and_completion(db):
    case=seed_original_composition(db)
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(case['Tenant'],))
    binding=json.loads(case['Params'])['base']
    old=binding['asset_version_id']
    db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,version_no) SELECT tenant_id,asset_id,master_artifact_id,content_hash,'{}','accepted',2 FROM photo.asset_versions WHERE id=%s",(old,))
    document=str(uuid4());digest='e'*64;size=321
    def api(query,args):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
            try:return db.execute(query,args).fetchone()
            finally:db.execute('RESET ROLE')
    args=[case['Tenant'],old,document,case['Actor'],case['Session'],digest,size,'test']
    def reserve(values=None,legacy=False):
        name='reserve_v2_draft_upload' if legacy else 'reserve_v27_semantic_intent_upload'
        return api('SELECT * FROM photo.'+name+'('+','.join(['%s']*8)+')',values or args)
    def usage():return db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['Tenant'],)).fetchone()
    before=usage()
    assert reserve(legacy=True)[0]=='conflict'
    first=reserve();assert first[0]=='created' and first[3] is False
    assert usage()==(before[0],before[1]+size)
    second=reserve();assert second[0]=='resumed' and second[1]==first[1] and second[2]!=first[2]
    assert usage()==(before[0],before[1]+size)
    changed=args.copy();changed[5]='f'*64
    assert reserve(changed)[0]=='conflict'
    def complete(token):
        values=[case['Tenant'],old,document,case['Actor'],case['Session'],second[1],token,digest,size,'test','fixture-version']
        return api('SELECT photo.complete_v27_semantic_intent_upload('+','.join(['%s']*11)+')',values)[0]
    assert complete(first[2]) is False
    assert complete(second[2]) is True
    assert usage()==(before[0]+size,before[1])
    replay=reserve();assert replay[0]=='replayed' and replay[1:3]==second[1:3] and replay[3] is True
    assert complete(second[2]) is True
    assert usage()==(before[0]+size,before[1])
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],))
    assert reserve()[0]=='forbidden'
