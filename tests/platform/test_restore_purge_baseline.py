from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.runtime import restore_purge_baseline as module


@pytest.mark.parametrize('mode',['valid','missing_namespace','foreign_tenant','missing_s3_version','inventory_failure','api_failure','s3_failure','scope_failure','inactive','fixture_denied'])
def test_baseline_covers_all_namespaces_and_matches_s3_version(monkeypatch,mode):
    tenant=uuid4();events=[]
    refs=tuple(sorted((module.RestoreObject('bucket',prefix+'tenants/'+str(tenant)+'/fixture','v1','a'*64,1)
                      for prefix in ('','staging/','originals/')),key=lambda ref:(ref.bucket,ref.key,ref.version)))
    s3=module.RestoreS3AccessTarget('https://restore.test','bucket',tenant,'tenants/'+str(tenant)+'/fixture',
        'wrong' if mode=='missing_s3_version' else 'v1','control','v2','a'*64,'private','private')
    if mode=='missing_namespace':refs=(refs[0],refs[0],refs[-1])
    if mode=='foreign_tenant':refs=(*refs[:-1],module.RestoreObject('bucket','tenants/'+str(uuid4())+'/fixture','v1','a'*64,1))
    monkeypatch.setattr(module,'MinioRestoreObjects',lambda *args,**kwargs:SimpleNamespace(open_version=object()))
    def adapter(name):
        def run(*args,**kwargs):
            kwargs['check_active']();events.append(name)
            if mode==name+'_failure':raise RuntimeError('failed observation')
            return name
        return run
    monkeypatch.setattr(module,'verify_restore_inventory',adapter('inventory'))
    monkeypatch.setattr(module,'observe_restore_access_readable',adapter('api'))
    monkeypatch.setattr(module,'observe_signed_s3_readable',adapter('s3'))
    def authority():
        if mode=='scope_failure':raise RuntimeError('wrong fixture')
        if mode=='fixture_denied':return False
    def run():return module.verify_purge_fixture_baseline(object(),['bucket'],tenant,refs,
        api_target=object(),api_sha256='a'*64,s3_target=s3,tls_context=object(),
        assert_installed_fixture=authority,check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':
        result=run();assert result.tenant_id==tenant and events==['inventory','api','s3']
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        if mode in ('missing_namespace','foreign_tenant','missing_s3_version','scope_failure','inactive','fixture_denied'):assert not events
