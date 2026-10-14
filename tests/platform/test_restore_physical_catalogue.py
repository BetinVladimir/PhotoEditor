from types import SimpleNamespace
import pytest
from photo_worker.runtime.restore_physical_catalogue import sorted_restore_versions


def item(key,version='v',size=5,marker=False):return SimpleNamespace(object_name=key,version_id=version,size=size,is_delete_marker=marker)
class Client:
    def __init__(self,items):self.items=items;self.calls=[];self.closed=False
    def list_objects(self,bucket,**options):
        self.calls.append((bucket,options))
        def rows():
            try:yield from self.items
            finally:self.closed=True
        return rows()


def test_physical_catalogue_orders_all_versions_on_disk_and_cleans_scratch(tmp_path):
    client=Client([item('z'),item('a','v2'),item('a','v1'),item('deleted',marker=True)])
    with sorted_restore_versions(client,['photos'],tmp_path,check_active=lambda:None) as rows:
        assert [(v.key,v.version) for v in rows]==[('a','v1'),('a','v2'),('z','v')]
        assert len(list(tmp_path.iterdir()))==1
    assert list(tmp_path.iterdir())==[] and client.closed
    assert client.calls==[('photos',{'recursive':True,'include_version':True})]


def test_inactive_catalogue_does_not_read_scratch_root(tmp_path,monkeypatch):
    original_resolve=type(tmp_path).resolve
    def resolve(path,*args,**kwargs):
        if path==tmp_path:raise AssertionError('scratch root read before active guard')
        return original_resolve(path,*args,**kwargs)
    monkeypatch.setattr(type(tmp_path),'resolve',resolve)
    client=Client([])
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        with sorted_restore_versions(client,['photos'],tmp_path,check_active=lambda:False):
            pass
    assert client.calls==[]


@pytest.mark.parametrize('mode',['duplicate','null_version','negative_size','limit','cancel','listing_failure'])
def test_physical_catalogue_refuses_incomplete_listing_and_removes_spool(tmp_path,mode):
    values=[item('a')];kwargs={};calls=0
    if mode=='duplicate':values*=2
    if mode=='null_version':values=[item('a','null')]
    if mode=='negative_size':values=[item('a',size=-1)]
    if mode=='limit':values=[item('a'),item('b')];kwargs['maximum_entries']=1
    client=Client(values)
    if mode=='listing_failure':
        def broken(*args,**kwargs):
            yield item('a')
            raise RuntimeError('listing failed')
        client.list_objects=broken
    def active():
        nonlocal calls
        calls+=1
        if mode=='cancel' and calls==3:raise RuntimeError('cancelled')
    with pytest.raises((ValueError,RuntimeError)):
        with sorted_restore_versions(client,['photos'],tmp_path,check_active=active,**kwargs) as rows:list(rows)
    assert list(tmp_path.iterdir())==[]


@pytest.mark.parametrize('denial_at',[1,3,5])
def test_physical_catalogue_explicit_false_refuses_and_removes_spool(tmp_path,denial_at):
    client=Client([item('a')]);checks=0
    def active():
        nonlocal checks
        checks+=1
        return False if checks==denial_at else None
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        with sorted_restore_versions(client,['photos'],tmp_path,check_active=active) as rows:
            list(rows)
    assert list(tmp_path.iterdir())==[]
    if denial_at==1:assert client.calls==[]
    else:assert client.closed
