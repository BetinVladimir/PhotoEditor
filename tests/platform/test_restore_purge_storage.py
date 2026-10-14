from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.runtime import restore_purge_storage as module


@pytest.mark.parametrize('mode',['valid','object','late_object','multipart','truncated','foreign_bucket',
    'malformed_uploads','listing_error','multipart_error','wrong_sdk','scope_error','cancelled'])
def test_python_minio_storage_probe_all_buckets_namespaces_and_passes(monkeypatch,mode):
    tenant=uuid4();events=[];closed=[]
    class Client:
        def list_objects(self,bucket,**kwargs):
            events.append(('versions',bucket,kwargs['prefix']))
            assert kwargs['recursive'] is True and kwargs['include_version'] is True
            def rows():
                try:
                    if mode=='listing_error':raise RuntimeError('storage unavailable')
                    if mode=='object' or (mode=='late_object' and len(events)>12):yield SimpleNamespace(is_delete_marker=True)
                finally:closed.append(True)
            return rows()
        def _list_multipart_uploads(self,bucket,**kwargs):
            events.append(('uploads',bucket,kwargs['prefix']))
            assert kwargs['max_uploads']==1 and kwargs['delimiter']==''
            if mode=='multipart_error':raise RuntimeError('multipart unavailable')
            return SimpleNamespace(bucket_name='foreign' if mode=='foreign_bucket' else bucket,
                is_truncated=mode=='truncated',uploads=None if mode=='malformed_uploads' else ([object()] if mode=='multipart' else []))
    monkeypatch.setattr(module.minio,'Minio',Client)
    monkeypatch.setattr(module.minio,'__version__','wrong' if mode=='wrong_sdk' else '7.2.18')
    def scope(actual,buckets):
        assert actual==tenant and buckets==('bucket-a','bucket-b')
        if mode=='scope_error':raise RuntimeError('scope invalid')
    def active():
        if mode=='cancelled' and events:raise RuntimeError('cancelled')
    def run():return module.verify_purged_tenant_storage(Client(),['bucket-b','bucket-a'],tenant,
        assert_fixture_scope=scope,check_active=active)
    if mode=='valid':
        result=run();assert result.passes==2 and len(events)==24 and len(closed)==12
        assert len({prefix for _,_,prefix in events})==3
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        if mode in ('wrong_sdk','scope_error'):assert not events


@pytest.mark.parametrize('denial',['activity','fixture'])
def test_purge_storage_explicit_false_refuses_before_listing(monkeypatch,denial):
    tenant=uuid4();calls=[]
    class Client:
        def list_objects(self,*args,**kwargs):calls.append('list');raise AssertionError('listing reached')
    monkeypatch.setattr(module.minio,'Minio',Client)
    monkeypatch.setattr(module.minio,'__version__','7.2.18')
    with pytest.raises(RuntimeError):
        module.verify_purged_tenant_storage(Client(),['bucket-a'],tenant,
            assert_fixture_scope=lambda *_:False if denial=='fixture' else None,
            check_active=lambda:False if denial=='activity' else None)
    assert calls==[]
