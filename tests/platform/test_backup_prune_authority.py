"""No deletion on shared-base/pinned-key authority failures; not executed."""
from datetime import datetime,timedelta,timezone
from hashlib import sha256
import sqlite3
from uuid import UUID
import pytest
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding,PublicFormat
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.backup_catalogue import prune_checkpoints


class NoSideEffects:
    def __getattr__(self,name):
        raise AssertionError('unexpected external operation: '+name)


def signing():
    key=Ed25519PrivateKey.generate()
    return key,sha256(key.public_key().public_bytes(Encoding.Raw,PublicFormat.Raw)).hexdigest()


def catalogue():
    db=sqlite3.connect(':memory:')
    db.execute('CREATE TABLE checkpoints(id TEXT PRIMARY KEY,envelope BLOB,version TEXT,state TEXT)')
    return db


def add(db,key,pin,identifier,stamp,state,label):
    manifest={'backup_id':str(identifier),'recovered_at':stamp.isoformat(),
        'signing_public_key_sha256':pin,'postgres':{'type':'full','label':label}}
    envelope=canonical_bytes({'manifest':manifest,'signature':key.sign(canonical_bytes(manifest)).hex()})
    db.execute('INSERT INTO checkpoints VALUES(?,?,?,?)',(str(identifier),envelope,'immutable',state));db.commit()


def prune(db,key,pin):
    return prune_checkpoints(db,NoSideEffects(),'backups',key,public_key_pin=pin,
        check_active=lambda:None,commands=NoSideEffects(),pgbackrest_config='/unused',
        pgbackrest_config_sha256='a'*64,stanza='photo')


def test_withdrawing_checkpoint_cannot_expire_base_shared_with_retained_point():
    key,pin=signing();now=datetime.now(timezone.utc)
    with catalogue() as db:
        add(db,key,pin,UUID(int=1),now,'published','20261017-120000F')
        add(db,key,pin,UUID(int=2),now-timedelta(days=50),'withdrawing','20261017-120000F')
        with pytest.raises(ValueError,match='BACKUP_DUPLICATE_BASE_AUTHORITY|BACKUP_RETAINED_BASE_AUTHORITY'):
            prune(db,key,pin)
        assert db.execute("SELECT count(*) FROM checkpoints WHERE state='pruned'").fetchone()[0]==0


def test_prune_rejects_actual_key_mismatch_before_any_external_operation():
    key,_=signing()
    with catalogue() as db:
        with pytest.raises(ValueError,match='BACKUP_PRUNE_SIGNING_KEY_PIN'):
            prune(db,key,'0'*64)
