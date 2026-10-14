from copy import deepcopy
from hashlib import sha256
from uuid import uuid4
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes


def test_sql_exposure_inventory_digest_matches_worker_canonical_bytes(db):
    task=uuid4()
    entries=[dict(contract=contract,ref=dict(artifact_id=str(uuid4()),sha256=str(index)*64))
        for index,contract in enumerate(('Params','WorkingImage','EditIntent','TransferCandidate','MaskRef','ResolvedTransform','NpyArray'))]
    params=entries[0]['ref']
    entries.sort(key=lambda entry:entry['ref']['artifact_id'])
    def hashes(rows):
        return (sha256(canonical_bytes(dict(entries=rows,operation='semantic.local_exposure',task=str(task),version='1'))).hexdigest(),
            sha256(canonical_bytes(rows)).hexdigest())
    input_digest,entries_sha=hashes(entries)
    def ready(rows=entries,digest=input_digest,sha=entries_sha):
        return db.execute('SELECT photo.semantic_exposure_inventory_digest_ready(%s,%s,%s,%s,%s,%s)',
            (task,params['artifact_id'],params['sha256'],digest,sha,Jsonb(rows))).fetchone()[0]
    assert ready() is True
    assert ready(digest='a'*64) is False
    assert ready(sha='a'*64) is False
    nodes=[dict(id=str(task),operation='semantic.local_exposure',operation_version='1',params_artifact_id=params['artifact_id'],params_sha=params['sha256'],input_digest=input_digest)]
    transport=[dict(task_id=str(task),entry_count=len(entries),entries_sha=entries_sha,direct_roots=[e['ref'] for e in entries])]
    inventory=[dict(task_id=str(task),entries=entries)]
    def batch(n=nodes,t=transport,i=inventory):
        return db.execute('SELECT photo.semantic_exposure_inventory_batch_ready(%s,%s,%s)',(Jsonb(n),Jsonb(t),Jsonb(i))).fetchone()[0]
    assert batch() is True
    assert batch(i=[]) is False
    assert batch(n=nodes+nodes,t=transport+transport,i=inventory+inventory) is False
    assert batch(i=[{**inventory[0],'task_id':str(uuid4())}]) is False
    assert batch(i=[{**inventory[0],'extra':1}]) is False
    assert batch(n=[{**nodes[0],'input_digest':'f'*64}]) is False
    assert batch(n=[{**nodes[0],'operation':'semantic.transfer'}]) is False
    assert batch(t=[{**transport[0],'entry_count':len(entries)-1}]) is False
    assert batch(t=[{**transport[0],'direct_roots':transport[0]['direct_roots'][::-1]}]) is False
    for mutation in ('reverse','duplicate','contract','params','extra','ref_extra','unicode','null'):
        rows=deepcopy(entries)
        if mutation=='reverse':rows.reverse()
        elif mutation=='duplicate':rows.append(rows[-1])
        elif mutation=='contract':rows[0]['contract']='InputClosureManifest'
        elif mutation=='params':next(row for row in rows if row['contract']=='Params')['ref']['sha256']='f'*64
        elif mutation=='extra':rows[0]['extra']=1
        elif mutation=='ref_extra':rows[0]['ref']['extra']=1
        elif mutation=='unicode':rows[0]['contract']='NpyArrayé'
        elif mutation=='null':rows[0]['ref']['sha256']=None
        digest,sha=hashes(rows)
        assert ready(rows,digest,sha) is False,mutation
    signature='photo.semantic_exposure_inventory_digest_ready(uuid,uuid,text,text,text,jsonb)'
    for role in ('photo_api','photo_scheduler','photo_worker'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]
