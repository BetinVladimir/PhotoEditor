from contextlib import contextmanager
from dataclasses import replace
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactRef, AdoptionInvalid
from photo_worker.v2.delivery_metadata_reader import MetadataResultScope
from photo_worker.v2.delivery_metadata_authority import SQLMetadataResultAuthority


def fixture():
    scope=MetadataResultScope(uuid4(),uuid4(),2,3,uuid4());ref=ArtifactRef(uuid4(),'a'*64)
    class Connection:
        autocommit=True
        inside=False
        committed=False
        calls=[]
        row=('photos','key','v1',ref.sha256,10,'image/jpeg','encoded_image','output')
        rows=[(uuid4(),'b'*64,'ready') for _ in range(3)]
        @contextmanager
        def transaction(self):
            self.inside=True
            try:yield
            finally:self.inside=False
            self.committed=True
        def execute(self,query,args):
            assert self.inside
            self.calls.append((query,args))
            return SimpleNamespace(fetchone=lambda:self.row,fetchall=lambda:self.rows)
    connection=Connection()
    return SQLMetadataResultAuthority(connection,scope=scope),connection,scope,ref


def test_authority_commits_tenant_scope_before_returning_location():
    authority,connection,scope,ref=fixture()
    value=authority.location(scope,ref)
    assert value.scope==scope and value.evidence.ref==ref
    assert connection.committed and not connection.inside
    assert connection.calls[0][1]==(str(scope.tenant_id),)
    assert connection.calls[1][1]==(scope.tenant_id,scope.task_id,2,3,scope.result_id,ref.artifact_id)


def test_foreign_scope_refused_before_database():
    authority,connection,scope,ref=fixture()
    with pytest.raises(AdoptionInvalid):authority.location(replace(scope,tenant_id=uuid4()),ref)
    assert connection.calls==[]


@pytest.mark.parametrize('mode',['missing','digest','output-count','output-state','duplicate'])
def test_authority_refuses_missing_or_inexact_result(mode):
    authority,connection,scope,ref=fixture()
    if mode=='missing':connection.row=None
    if mode=='digest':connection.row=connection.row[:3]+('c'*64,)+connection.row[4:]
    if mode=='output-count':connection.rows=[]
    if mode=='output-state':connection.rows[0]=(uuid4(),'b'*64,'pending')
    if mode=='duplicate':connection.rows=[connection.rows[0]]*3
    with pytest.raises(AdoptionInvalid):
        if mode in ('missing','digest'):authority.location(scope,ref)
        else:authority.outputs(scope)
    assert not connection.inside
