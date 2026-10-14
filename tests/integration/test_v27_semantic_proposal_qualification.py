"""Imported metadata and transaction fences; not GPU quality qualification."""
from copy import deepcopy
import psycopg
import pytest
from psycopg.types.json import Jsonb
from layer_original_fixture import seed_original_composition
from semantic_qualification_fixture import frozen_qualification


def test_proposal_qualification_live_pin_and_locks(database):
    with psycopg.connect(**database) as setup:
        case=seed_original_composition(setup)
        pin=frozen_qualification(setup,case['Tenant'],case['Project'],case['Actor'])
        setup.commit()
    with psycopg.connect(**database) as reader:
        reader.execute('SET LOCAL ROLE photo_api')
        reader.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
        rows=reader.execute('SELECT contract,artifact_id,sha256 FROM photo.semantic_proposal_qualification_objects(%s,%s,%s,%s,%s)',(case['Tenant'],case['Project'],case['Actor'],case['Session'],Jsonb(pin))).fetchall()
        assert [(contract,str(aid),sha) for contract,aid,sha in rows]==[('Calibration',pin['calibration']['artifact_id'],pin['calibration']['sha256']),('Report',pin['report']['artifact_id'],pin['report']['sha256'])]
        bad=deepcopy(pin);bad['report']['sha256']='0'*64
        assert reader.execute('SELECT * FROM photo.semantic_proposal_qualification_objects(%s,%s,%s,%s,%s)',(case['Tenant'],case['Project'],case['Actor'],case['Session'],Jsonb(bad))).fetchall()==[]
        reader.rollback()
    query='SELECT photo.lock_semantic_proposal_qualification(%s,%s,%s,%s,%s)'
    def args(value):return (case['Tenant'],case['Project'],case['Actor'],case['Session'],Jsonb(value))
    with psycopg.connect(**database) as locked,psycopg.connect(**database) as writer:
        locked.execute('SET LOCAL ROLE photo_scheduler')
        locked.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
        assert locked.execute(query,args(pin)).fetchone()[0] is True
        for field,value in [('backend','cpu'),('model_lock_sha','0'*64),('policy_id','invalid'),('policy_sha',3)]:
            bad=deepcopy(pin);bad[field]=value
            assert locked.execute(query,args(bad)).fetchone()[0] is False
        for statement,values in [
            ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['report']['artifact_id'],)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['calibration']['artifact_id'],)),
        ]:
            writer.execute("SET LOCAL lock_timeout='250ms'")
            with pytest.raises(psycopg.errors.LockNotAvailable):writer.execute(statement,values)
            writer.rollback()
        locked.rollback()
        writer.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],));writer.commit()
        locked.execute('SET LOCAL ROLE photo_scheduler')
        locked.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
        assert locked.execute(query,args(pin)).fetchone()[0] is False
        signature='photo.lock_semantic_proposal_qualification(uuid,uuid,uuid,uuid,jsonb)'
        assert writer.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE'),has_function_privilege('public',%s,'EXECUTE'),has_function_privilege('photo_worker',%s,'EXECUTE'),has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)*4).fetchone()==(False,False,False,True)
