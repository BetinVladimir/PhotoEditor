"""SQL policy fingerprint matches the canonical cross-language representation."""
from uuid import uuid4
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from test_database import tenant


def test_semantic_policy_hash_canonical_and_complete(db):
    _,actor=tenant(db,str(uuid4())+'@example.test')
    checks=['quality\n"','геометрия','𐐀','geometry']
    fixtures=['e'*64,'1'*64];calibrations=['f'*64];evidence=['0'*64,'2'*64,'3'*64]
    policy=db.execute("INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,calibration_shas,evidence_shas,license_cleared) VALUES('semantic.transfer','1',%s,%s,'cuda',%s,%s,%s,%s,%s,%s,%s,true) RETURNING id",('a'*64,'b'*64,'c'*64,'d'*64,actor,checks,fixtures,calibrations,evidence)).fetchone()[0]
    expected=dict(schema_version=1,policy_id=str(policy),operation='semantic.transfer',operation_version='1',backend='cuda',model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha='d'*64,issuer_id=str(actor),required_checks=sorted(checks),test_fixture_shas=sorted(fixtures),calibration_shas=sorted(calibrations),evidence_shas=sorted(evidence),license_cleared=True)
    read=lambda:db.execute('SELECT photo.v27_semantic_policy_sha(%s)',(policy,)).fetchone()[0]
    original=sha256(canonical_bytes(expected)).hexdigest()
    assert read()==original
    db.execute('UPDATE photo.v2_qualification_policies SET required_checks=%s,test_fixture_shas=%s WHERE id=%s',(list(reversed(checks)),list(reversed(fixtures)),policy))
    assert read()==original
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.v2_qualification_policies SET required_checks=ARRAY['new_requirement'] WHERE id=%s",(policy,))
        assert read()!=original
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.v2_qualification_policies SET required_checks=ARRAY['quality','quality'] WHERE id=%s",(policy,))
        assert read() is None
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.v2_qualification_policies SET license_cleared=false WHERE id=%s',(policy,))
        assert read() is None
    for role in ('photo_scheduler','photo_api','photo_worker','public'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_semantic_policy_sha(uuid)','EXECUTE')",(role,)).fetchone()[0]
