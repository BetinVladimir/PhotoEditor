from dataclasses import replace
from datetime import datetime,timezone
from uuid import uuid4
import pytest
from photo_worker.runtime import restore_purge_observation as module


@pytest.mark.parametrize('mode',['valid','metadata_failure','storage_failure','api_failure','s3_failure','changed_metadata','changed_storage','scope_failure','inactive','fixture_denied'])
def test_combined_purge_observes_all_boundaries_and_rechecks(monkeypatch,mode):
    tenant,job=uuid4(),uuid4();events=[];counts={}
    target=module.RestoreS3AccessTarget('https://restore.test','bucket',tenant,'key','v1','control','v2','a'*64,'private','private')
    metadata=module.RestorePurgeMetadataEvidence('restore-purge-metadata/v1','isolated',tenant,job,('tasks',))
    storage=module.RestorePurgeStorageEvidence('restore-purge-storage/v1',tenant,('bucket',),('prefix',),2)
    api=module.RestoreAccessDenialEvidence('restore-access-denial/v1',403,'FORBIDDEN','a'*64,datetime.now(timezone.utc))
    s3=module.RestoreS3DenialEvidence('restore-s3-denial/v1',tenant,'bucket','key','v1','NoSuchVersion','b'*64,datetime.now(timezone.utc))
    def adapter(name,result):
        def run(*args,**kwargs):
            kwargs['check_active']();events.append(name);counts[name]=counts.get(name,0)+1
            if mode==name+'_failure':raise RuntimeError('observer failed')
            if mode=='changed_metadata' and name=='metadata' and counts[name]>1:return replace(result,checked_tables=('different',))
            if mode=='changed_storage' and name=='storage' and counts[name]>1:return replace(result,buckets=('different',))
            return result
        return run
    monkeypatch.setattr(module,'verify_purged_tenant_metadata',adapter('metadata',metadata))
    monkeypatch.setattr(module,'verify_purged_tenant_storage',adapter('storage',storage))
    monkeypatch.setattr(module,'observe_restore_access_denied',adapter('api',api))
    monkeypatch.setattr(module,'observe_purged_signed_s3',adapter('s3',s3))
    def authority():
        if mode=='scope_failure':raise RuntimeError('scope invalid')
        if mode=='fixture_denied':return False
    def run():return module.observe_completed_fixture_purge(object(),object(),['bucket'],tenant,job,
        expected_database='isolated',api_target=object(),s3_target=target,tls_context=object(),
        assert_installed_fixture=authority,check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':
        result=run();assert result.metadata==metadata and result.storage==storage
        assert events==['metadata','storage','api','s3','metadata','storage']
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        if mode in ('scope_failure','inactive','fixture_denied'):assert not events
