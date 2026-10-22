"""Actual mounted adoption loop: deployment withdrawal never rejects valid bytes.
DB/object IO are boundary doubles; no SQL/physical qualification is claimed.
"""
from contextlib import nullcontext
from threading import Event
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.service import Adopter
from photo_worker.v2.adoption import AdoptionInvalid

DEPENDENCIES = (
 'LOCAL_PORTRAIT_SCRATCH_REQUIRED','EDITOR_SEAL_ADOPTION_SCRATCH_REQUIRED',
 'REGION_REPLAY_INSTALLATION_REQUIRED','INTERCHANGE_ADOPTION_SCRATCH_REQUIRED',
 'RELIGHT_ADOPTION_INSTALLATION_REQUIRED','PERSPECTIVE_LINES_ADOPTION_INSTALLATION_REQUIRED',
 'MULTI_ADOPTION_INSTALLATION_REQUIRED','PERSPECTIVE_ADOPTION_INSTALLATION_REQUIRED',
 'NEGATIVE_ADOPTION_INSTALLATION_REQUIRED','SEMANTIC_SHOWN_INSTALLATION_REQUIRED',
 'SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED',
)


def mounted(code):
    tenant,run,task,result=uuid4(),uuid4(),uuid4(),uuid4()
    events=[]
    class Read:
        def execute(self,query,args=()):
            events.append(query)
            if query=='SELECT * FROM photo.v2_pending_adoptions()':return SimpleNamespace(fetchall=lambda:[(tenant,run,task)])
            if 'editor_effect_pending_seal_adoptions' in query or 'v2_pending_finalizations' in query:return SimpleNamespace(fetchall=lambda:[])
            if 'pg_try_advisory_lock' in query or 'pg_advisory_unlock' in query:return SimpleNamespace(fetchone=lambda:(True,))
            raise AssertionError(query)
    class Write:
        def transaction(self):return nullcontext()
        def execute(self,query,args=()):
            events.append(query)
            assert 'reject_v2_adoption' in query
            assert args==(tenant,task,result,code)
            return SimpleNamespace(fetchone=lambda:(True,))
    class Coordinator:
        def adopt(self,*args):raise AdoptionInvalid(code)
        def context(self,*args):
            events.append('terminal context')
            return SimpleNamespace(result_id=result)
    instance=object.__new__(Adopter)
    instance.stop=Event();instance.read=Read();instance.write=Write();instance.coordinator=Coordinator()
    instance.check_active=lambda:None
    for name in ('exposure_enabled','shown_exposure_enabled','shown_removal_enabled','shown_background_cleanup_enabled','shown_retouch_enabled'):setattr(instance,name,False)
    for name in ('delivery_encode','negative_review_publisher','derivative_coordinator','multi_quality_runner','multi_preview_runner','restoration_preview_runner','perspective_preview_runner','perspective_quality_runner','lens_preview_runner'):setattr(instance,name,None)
    return instance,events


@pytest.mark.parametrize('code',DEPENDENCIES)
def test_missing_specific_dependency_preserves_pending_result(code):
    adopter,events=mounted(code)
    assert adopter.run_once()=={'adopted':0,'rejected':0,'finalized':0,'retry':1}
    assert 'terminal context' not in events
    assert not any('reject_v2_adoption' in query for query in events)
    assert any('pg_advisory_unlock' in query for query in events)


def test_same_pending_result_can_be_adopted_after_dependency_installation():
    adopter,events=mounted('LOCAL_PORTRAIT_SCRATCH_REQUIRED')
    assert adopter.run_once()=={'adopted':0,'rejected':0,'finalized':0,'retry':1}
    # The pending selector returns the same immutable task/result. Installing
    # its dependency changes the available verifier, not the accepted input.
    adopted=[]
    def installed(tenant,task):
        adopted.append((tenant,task))
        return True
    adopter.coordinator.adopt=installed
    assert adopter.run_once()=={'adopted':1,'rejected':0,'finalized':0,'retry':0}
    assert len(adopted)==1
    assert 'terminal context' not in events
    assert not any('reject_v2_adoption' in query for query in events)


@pytest.mark.parametrize('code',['LOCAL_PORTRAIT_PROTECTED_PIXELS_CHANGED','EDITOR_SEAL_ARRAY_HASH','UNKNOWN_INSTALLATION_REQUIRED'])
def test_invalid_physical_result_is_not_hidden_by_dependency_retry(code):
    adopter,events=mounted(code)
    assert adopter.run_once()=={'adopted':0,'rejected':1,'finalized':0,'retry':0}
    assert 'terminal context' in events
    assert any('reject_v2_adoption' in query for query in events)
