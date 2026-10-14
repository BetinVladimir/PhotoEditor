from uuid import uuid4,uuid5,UUID


def test_sql_go_namespace_identity_matches_python_uuid_v5(db):
    for namespace in (uuid4(),UUID(int=0),UUID('6ba7b810-9dad-11d1-80b4-00c04fd430c8')):
        for name in ('','Params','semantic.edit:'+str(uuid4()),'экспозиция/照片'):
            assert db.execute('SELECT photo.semantic_uuid_v5(%s,%s)',(namespace,name)).fetchone()[0]==uuid5(namespace,name)
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.semantic_approved_child_params_ready(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb)','EXECUTE')",(role,)).fetchone()[0]
