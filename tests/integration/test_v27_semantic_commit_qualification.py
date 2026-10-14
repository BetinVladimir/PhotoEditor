"""Qualification metadata fence locks; not physical GPU/model evidence."""
from uuid import uuid4
import psycopg
import pytest
from psycopg.types.json import Jsonb
from test_v27_semantic_final_producer_gate import fixture


def test_semantic_commit_retains_qualification_revocation_locks(database):
    with psycopg.connect(**database) as setup:
        tid,task,_,_,_,_,scope=fixture(setup,snapshot_authority=True,qualification=True)
        run=setup.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(task,)).fetchone()[0]
        pin=scope['qualification']
        setup.commit()
    with psycopg.connect(**database) as publication,psycopg.connect(**database) as revocation:
        publication.execute('SET LOCAL ROLE photo_scheduler')
        # An invalid lease cannot publish a graph. Even this denied publication
        # crosses the live qualification fence, whose locks remain until rollback.
        assert not publication.execute('SELECT photo.commit_v27_semantic_assembly(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tid,run,uuid4(),uuid4(),task,Jsonb([dict(contract='Calibration',ref=pin['calibration'])]),Jsonb({}),Jsonb([]),'a'*64,Jsonb([]))).fetchone()[0]
        for statement,args in [
            ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
            ("UPDATE photo.v2_qualification_policies SET required_checks=ARRAY['changed'] WHERE id=%s",(pin['policy_id'],)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['report']['artifact_id'],)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['calibration']['artifact_id'],)),
        ]:
            revocation.execute("SET LOCAL lock_timeout='150ms'")
            with pytest.raises(psycopg.errors.LockNotAvailable):
                revocation.execute(statement,args)
            revocation.rollback()
        publication.rollback()
        revocation.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],))
        revocation.commit()
        publication.execute('SET LOCAL ROLE photo_scheduler')
        assert not publication.execute('SELECT photo.v27_semantic_frozen_qualification_ready(%s,%s)',(tid,run)).fetchone()[0]
