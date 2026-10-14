"""Direct restore mutators must fail closed before opening a SQL transaction."""
import pytest

from photo_worker.runtime.restore_authority import require_restore_authority
from photo_worker.runtime.restore_cleanup import fence_restored_cleanup
from photo_worker.runtime.restore_intents import invalidate_expired_restore_intents
from photo_worker.runtime.restore_outbox import prepare_restore_outbox
from photo_worker.runtime.restore_mail import recover_restored_mail
from photo_worker.runtime.restore_leases import fence_restored_compute_leases
from photo_worker.runtime.restore_resources import recover_restored_resources
from photo_worker.runtime.restore_scheduler_grants import fence_restored_scheduler_grants


_MUTATORS=(fence_restored_cleanup,invalidate_expired_restore_intents,
           prepare_restore_outbox,recover_restored_mail,
           fence_restored_compute_leases,recover_restored_resources,
           fence_restored_scheduler_grants)


@pytest.mark.parametrize('denial',['activity_before','isolation','activity_after'])
def test_shared_authority_rejects_each_boolean_denial(denial):
    calls=[]
    def active():
        calls.append('active')
        return False if denial=='activity_before' and calls.count('active')==1 or denial=='activity_after' and calls.count('active')==2 else None
    def isolated():
        calls.append('isolation')
        return denial!='isolation'
    with pytest.raises(RuntimeError):require_restore_authority(active,isolated)
    assert calls==(['active'] if denial=='activity_before' else
                   ['active','isolation'] if denial=='isolation' else
                   ['active','isolation','active'])


@pytest.mark.parametrize('mutator',_MUTATORS)
@pytest.mark.parametrize('denial',['activity','isolation'])
def test_direct_restore_mutator_refuses_false_before_sql(mutator,denial):
    class NoSQL:
        def __getattribute__(self,name):
            raise AssertionError('SQL or transaction touched before authority')
    with pytest.raises(RuntimeError):
        mutator(NoSQL(),expected_database='isolated',
            assert_isolated_and_paused=lambda:False if denial=='isolation' else None,
            check_active=lambda:False if denial=='activity' else None)
