import json
from pathlib import Path
from psycopg.types.json import Jsonb


def test_negative_sql_resources_and_exact_profile(db):
    root=Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata'
    cases=json.loads((root/'negative-resource-parity.json').read_text())
    pin=json.loads((root/'negative-qualification.json').read_text())['pin']
    for case in cases:
        resources=db.execute('SELECT photo.v29_negative_resources(%s,%s,%s)',
            (case['Width'],case['Height'],case['Alpha'])).fetchone()[0]
        assert resources==case['Expected']
        profile=dict(backend='cpu',renderer_version='v2.2',negative_runtime_sha=pin['runtime_lock_sha'],
            negative_qualification=pin,resource_estimate=resources)
        args=(case['Width'],case['Height'],case['Alpha'],pin['runtime_lock_sha'],Jsonb(pin))
        assert db.execute('SELECT photo.v29_negative_profile_shape(%s,%s,%s,%s,%s,%s)',args+(Jsonb(profile),)).fetchone()==(True,)
        profile['resource_estimate']={**resources,'ram_bytes':resources['ram_bytes']-1}
        assert db.execute('SELECT photo.v29_negative_profile_shape(%s,%s,%s,%s,%s,%s)',args+(Jsonb(profile),)).fetchone()==(False,)
    for values in [(0,1,False),(200000001,1,False),(1,1,None),(9223372036854775807,9223372036854775807,True)]:
        assert db.execute('SELECT photo.v29_negative_resources(%s,%s,%s)',values).fetchone()==(None,)
    for signature in ['photo.v29_negative_resources(bigint,bigint,boolean)',
            'photo.v29_negative_profile_shape(bigint,bigint,boolean,text,jsonb,jsonb)']:
        assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE'),has_function_privilege('photo_worker',%s,'EXECUTE'),has_function_privilege('public',%s,'EXECUTE')",(signature,)*3).fetchone()==(True,False,False)
