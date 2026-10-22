"""Dedicated seal authority refuses missing origins; authored, execution deferred."""
from uuid import uuid4
import pytest


def test_unknown_seal_has_no_adoption_context_or_evidence(db):
    tenant,task,artifact,result=(uuid4() for _ in range(4))
    assert db.execute('SELECT * FROM photo.editor_effect_seal_adoption_context(%s,%s)',(tenant,task)).fetchone() is None
    assert db.execute('SELECT * FROM photo.editor_effect_seal_adoption_artifact(%s,%s,1,1,%s)',(tenant,task,artifact)).fetchone() is None
    assert db.execute('SELECT photo.adopt_editor_effect_seal(%s,%s,1,1,%s,%s::jsonb,%s)',
        (tenant,task,result,'[{"artifact_id":"'+str(artifact)+'","sha256":"'+'a'*64+'","purpose":"'+'b'*64+'","ordinal":0}]','c'*64)).fetchone()[0] is False


@pytest.mark.parametrize('role', ['photo_api','photo_worker','photo_gateway'])
def test_only_scheduler_can_commit_seal(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.adopt_editor_effect_seal(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE')",(role,)).fetchone()[0] is False
    assert db.execute("SELECT has_function_privilege(%s,'photo.editor_effect_seal_adoption_scope(uuid,uuid,integer,bigint,uuid)','EXECUTE')",(role,)).fetchone()[0] is False
