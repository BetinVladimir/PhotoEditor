import pytest
from photo_worker.v2.relight_replay import verify_relight_replay


@pytest.mark.parametrize('io,scratch',[(False,1),(0,1),(1,False),(1,0),(2**53,1),(1,2**53)])
def test_replay_refuses_invalid_budgets_before_reader_io(tmp_path,io,scratch):
    with pytest.raises(ValueError,match='RELIGHT_REPLAY_SCRATCH_INVALID'):
        verify_relight_replay(None,None,None,scratch_root=tmp_path,max_bytes=io,max_scratch_bytes=scratch,
            execution_profile={},verify_authority=lambda *args:True,check_active=lambda:None)


def test_replay_never_runs_without_live_authority(tmp_path):
    with pytest.raises(ValueError,match='RELIGHT_REPLAY_AUTHORITY_REQUIRED'):
        verify_relight_replay(None,None,None,scratch_root=tmp_path,max_bytes=1,max_scratch_bytes=1,
            execution_profile={},verify_authority=None,check_active=lambda:None)


@pytest.mark.parametrize('context',[None,{},lambda:True])
def test_generic_adoption_refuses_relight_without_explicit_replay(context):
    from uuid import uuid4
    from photo_worker.v2.adoption import prepare_v2_adoption
    with pytest.raises(ValueError,match='RELIGHT_ADOPTION_CONTEXT_REQUIRED'):
        prepare_v2_adoption(task_id=uuid4(),operation='image.relight',version='1',root=None,reader=None,relight_context=context)
