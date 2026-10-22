from datetime import datetime,timedelta,timezone
from hashlib import sha256
import io
from types import SimpleNamespace
from uuid import uuid4
import pytest

pytest.importorskip('cryptography')
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding,PublicFormat
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.backup_catalogue import operator_catalogue,record_publication,prune_checkpoints


class Remote:
    def __init__(self):self.markers={};self.calls=[];self.fail=False
    def put_object(self,bucket,key,stream,size,**kwargs):
        self.calls.append(('withdraw',key));self.markers[key]=stream.read()
        return SimpleNamespace(version_id='withdraw-v1')
    def get_object(self,bucket,key,**kwargs):
        return SimpleNamespace(read=lambda size:self.markers[key],close=lambda:None,release_conn=lambda:None)
    def remove_object(self,bucket,key,**kwargs):
        self.calls.append(('delete',key,kwargs['version_id']))
        if self.fail:raise OSError('temporary network failure')


class PostgresRepository:
    def __init__(self,labels):self.labels=set(labels);self.calls=[]
    def run(self,name,args):
        self.calls.append((name,args))
        if args[-1]=='expire':
            self.labels.remove(next(x[6:] for x in args if x.startswith('--set=')))
            return b''
        return canonical_bytes([{'name':'photo','backup':[{'label':label,'type':'full'} for label in sorted(self.labels)]}])


def seed(catalogue,key,pin,stamp,label):
    identifier=str(uuid4())
    manifest={'backup_id':identifier,'recovered_at':stamp.isoformat(),'signing_public_key_sha256':pin,
        'postgres':{'label':label,'type':'full'},'objects':[
            {'name':name,'key':'checkpoints/'+identifier+'/'+name+'.tar.age','version_id':name+'-v1'}
            for name in ('minio','config','deletions')]}
    record_publication(catalogue,canonical_bytes({'manifest':manifest,'signature':key.sign(canonical_bytes(manifest)).hex()}),'commit-v1')
    return identifier


def test_withdrawal_precedes_exact_deletion_and_repository_expiration(tmp_path):
    root=tmp_path/'journal';root.mkdir(mode=0o700)
    pgconfig=tmp_path/'pg.conf';pgconfig.write_bytes(b'operator-installed')
    key=Ed25519PrivateKey.generate();pin=sha256(key.public_key().public_bytes(Encoding.Raw,PublicFormat.Raw)).hexdigest()
    now=datetime(2026,10,17,12,tzinfo=timezone.utc);remote=Remote();repository=PostgresRepository({'older','latest'})
    with operator_catalogue(root,check_active=lambda:True) as catalogue:
        older=seed(catalogue,key,pin,now-timedelta(hours=2),'older')
        seed(catalogue,key,pin,now-timedelta(hours=1),'latest')
        result=prune_checkpoints(catalogue,remote,'backups',key,public_key_pin=pin,check_active=lambda:True,
            commands=repository,pgbackrest_config=str(pgconfig),pgbackrest_config_sha256=sha256(pgconfig.read_bytes()).hexdigest(),stanza='photo',now=now)
        assert result['pruned']==[older]
        assert remote.calls[0]==('withdraw','checkpoints/'+older+'/WITHDRAWN.json')
        assert remote.calls[1]==('delete','checkpoints/'+older+'/COMMITTED.json','commit-v1')
        assert repository.labels=={'latest'}
        assert catalogue.execute('SELECT state FROM checkpoints WHERE id=?',(older,)).fetchone()==('pruned',)


def test_partial_delete_remains_withdrawn_for_retry(tmp_path):
    root=tmp_path/'journal';root.mkdir(mode=0o700)
    pgconfig=tmp_path/'pg.conf';pgconfig.write_bytes(b'operator-installed')
    key=Ed25519PrivateKey.generate();pin=sha256(key.public_key().public_bytes(Encoding.Raw,PublicFormat.Raw)).hexdigest()
    now=datetime(2026,10,17,12,tzinfo=timezone.utc);remote=Remote();remote.fail=True
    repository=PostgresRepository({'older','latest'})
    with operator_catalogue(root,check_active=lambda:True) as catalogue:
        older=seed(catalogue,key,pin,now-timedelta(hours=2),'older')
        seed(catalogue,key,pin,now-timedelta(hours=1),'latest')
        with pytest.raises(OSError):
            prune_checkpoints(catalogue,remote,'backups',key,public_key_pin=pin,check_active=lambda:True,
                commands=repository,pgbackrest_config=str(pgconfig),pgbackrest_config_sha256=sha256(pgconfig.read_bytes()).hexdigest(),stanza='photo',now=now)
        assert catalogue.execute('SELECT state FROM checkpoints WHERE id=?',(older,)).fetchone()==('withdrawing',)
        assert repository.labels=={'older','latest'}
