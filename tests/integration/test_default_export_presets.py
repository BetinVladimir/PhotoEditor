from uuid import uuid4


def test_new_tenant_gets_exact_master_and_web_presets(db):
    user=uuid4();tenant=uuid4()
    db.execute("INSERT INTO photo.users(id,email_normalized) VALUES(%s,%s)",(user,f'{user}@example.test'))
    db.execute("INSERT INTO photo.tenants(id,name,owner_user_id,quotas,retention) VALUES(%s,'Seed check',%s,'{}','{}')",(tenant,user))
    rows=db.execute("SELECT name,config,config_hash,revision FROM photo.export_presets WHERE tenant_id=%s ORDER BY name",(tenant,)).fetchall()
    assert [row[0] for row in rows]==['Master','Web']
    master,web=rows
    assert master[1]['format']=='tiff' and master[1]['icc_id']=='work_rec2020_gamma24_v1' and master[1]['alpha']=='preserve'
    assert web[1]['format']=='jpeg' and web[1]['icc_id']=='srgb_v1' and web[1]['size']=={'long_edge':2048,'fit':'contain','allow_upscale':False}
    assert [row[2] for row in rows]==['53ce136942dc0e726e24ce51f6aafcd6fae5edace49b38a8534030720639f826','4d6d9b57d4318d1d0cd33677c5d31ca842c5e5fcd31cfd18c096896817d8b21a']
    assert [row[3] for row in rows]==[1,1]
