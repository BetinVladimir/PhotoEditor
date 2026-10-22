"""Closed reader routing; real SQL and physical bytes are separate packets."""
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef
from photo_worker.v2.adoption_store import PostgresObjectEvidenceReader

OPS=tuple('semantic.adopt_shown_'+n for n in ('exposure','removal','background_cleanup','retouch_region'))

class Connection:
    autocommit=True
    def __init__(self):self.calls=[];self.rows=[]
    def execute(self,sql,args):self.calls.append((sql,args));return self
    def fetchone(self):return ('bucket','key','version','a'*64,1,'application/json',None,'input')
    def fetchall(self):return self.rows

def reader(connection,**scope):
    return PostgresObjectEvidenceReader(connection,None,tenant_id=uuid4(),task_id=uuid4(),attempt=1,fence=2,allowed_bucket='bucket',**scope)

@pytest.mark.parametrize('operation',OPS)
def test_each_shown_operation_routes_both_reads_exactly(operation):
    db=Connection();r=reader(db,shown_operation=operation);ref=ArtifactRef(uuid4(),'a'*64)
    assert r.evidence(ref).ref==ref
    db.rows=[(ref.artifact_id,ref.sha256,'ready')]
    assert r.staged_outputs()=={ref}
    assert 'photo.v27_shown_adoption_artifact(' in db.calls[0][0]
    assert db.calls[0][1]==(r.tenant_id,r.task_id,1,2,ref.artifact_id,operation)
    assert 'photo.v27_shown_adoption_staged_outputs(' in db.calls[1][0]
    assert db.calls[1][1]==(r.tenant_id,r.task_id,1,2,operation)
    db.rows.append((uuid4(),'b'*64,'uploading'))
    with pytest.raises(AdoptionInvalid,match='STAGED_OUTPUT_INCOMPLETE'):r.staged_outputs()

@pytest.mark.parametrize('scope',[{'shown_operation':'semantic.other'}, {'shown_operation':OPS[0],'exposure':True}, {'shown_operation':OPS[0],'editor_seal':True}])
def test_shown_scope_is_closed_and_exclusive(scope):
    db=Connection()
    with pytest.raises(ValueError):reader(db,**scope)
    assert db.calls==[]

@pytest.mark.parametrize('scope,prefix',[({},'v2'),({'exposure':True},'v27_exposure'),({'editor_seal':True},'editor_effect_seal')])
def test_existing_reader_scopes_are_unchanged(scope,prefix):
    db=Connection();r=reader(db,**scope)
    r.evidence(ArtifactRef(uuid4(),'a'*64));r.staged_outputs()
    assert 'photo.'+prefix+'_adoption_artifact(' in db.calls[0][0]
    assert 'photo.'+prefix+'_adoption_staged_outputs(' in db.calls[1][0]
