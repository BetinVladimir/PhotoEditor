"""Private bounded key files and separately pinned historical witnesses."""
import base64
from hashlib import sha256
import os
from pathlib import Path
import re
import stat
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_key_probe import AESGCMRestoreKeyProbe,RestoreKeyWitness


def _open_private_descriptor(path,active):
    directory=os.open('/',os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:
        for component in path.parts[1:-1]:
            active()
            child=os.open(component,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,
                          dir_fd=directory)
            os.close(directory)
            directory=child
        active()
        return os.open(path.name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,
                       dir_fd=directory)
    finally:
        os.close(directory)


def _read_private(path,maximum,active):
    path=Path(path)
    if not callable(active):raise ValueError('RESTORE_KEY_ACTIVITY_GUARD')
    def ensure_active():
        if active() is False:raise RuntimeError('RESTORE_PRIVATE_READ_INACTIVE')
    ensure_active()
    if not path.is_absolute() or path.resolve(strict=True)!=path:
        raise ValueError('RESTORE_KEY_FILE_PATH')
    descriptor=_open_private_descriptor(path,ensure_active)
    try:
        before=os.fstat(descriptor)
        if (not stat.S_ISREG(before.st_mode) or before.st_nlink!=1
                or before.st_uid!=os.getuid() or before.st_mode & 0o077
                or not 1<=before.st_size<=maximum):raise ValueError('RESTORE_KEY_FILE_MODE')
        chunks=[];size=0
        while True:
            ensure_active();chunk=os.read(descriptor,min(65536,maximum+1-size))
            if not chunk:break
            chunks.append(chunk);size+=len(chunk)
            if size>maximum:raise ValueError('RESTORE_KEY_FILE_LIMIT')
        after=os.fstat(descriptor);current=path.lstat()
        identity=lambda s:(s.st_dev,s.st_ino,s.st_mode,s.st_nlink,s.st_uid,s.st_size,s.st_mtime_ns,s.st_ctime_ns)
        if (identity(before)!=identity(after) or identity(after)!=identity(current)
                or size!=before.st_size or path.resolve(strict=True)!=path):
            raise ValueError('RESTORE_KEY_FILE_CHANGED')
        ensure_active();return b''.join(chunks)
    finally:os.close(descriptor)


def _decode(value,maximum):
    if not isinstance(value,str) or len(value)>((maximum+2)//3)*4:
        raise ValueError('RESTORE_KEY_ENCODING')
    try:raw=base64.b64decode(value,validate=True)
    except Exception:raise ValueError('RESTORE_KEY_ENCODING') from None
    if len(raw)>maximum or base64.b64encode(raw).decode()!=value:
        raise ValueError('RESTORE_KEY_ENCODING')
    return raw


def load_restore_key_probe(keys_path,witness_path,witness_sha256,*,check_active):
    """Load exact canonical closed files; witness pin comes from operator authority.

    keys: {schema_version:restore-aes-keys/v1,entries:[{domain,key_id,key_b64}]}.
    witnesses: {schema_version:restore-aes-witnesses/v1,entries:[{domain,key_id,
    sealed_b64,aad_b64,plaintext_sha256}]}.
    Entries sorted (domain,key_id), exactly matching, max4096. Never log files.
    """
    if (not callable(check_active) or not isinstance(witness_sha256,str)
            or re.fullmatch('[0-9a-f]{64}',witness_sha256) is None):
        raise ValueError('RESTORE_KEY_PIN_INSTALLATION')
    def active():
        if check_active() is False:
            raise RuntimeError('RESTORE_KEY_INSTALLATION_INACTIVE')
    keys_raw=_read_private(keys_path,1<<20,active)
    witnesses_raw=_read_private(witness_path,8<<20,active)
    if sha256(witnesses_raw).hexdigest()!=witness_sha256:
        raise ValueError('RESTORE_KEY_WITNESS_PIN')
    def document(raw,schema,limit):
        try:value=parse_json(raw,max_bytes=limit)
        except Exception:raise ValueError('RESTORE_KEY_DOCUMENT') from None
        if (not isinstance(value,dict) or set(value)!= {'schema_version','entries'}
                or value['schema_version']!=schema or canonical_bytes(value)!=raw
                or not isinstance(value['entries'],list) or not 1<=len(value['entries'])<=4096):
            raise ValueError('RESTORE_KEY_DOCUMENT')
        return value['entries']
    def catalogue(entries,fields):
        result={};previous=None
        for entry in entries:
            active()
            if (not isinstance(entry,dict) or set(entry)!=fields
                    or entry['domain'] not in ('mail','capture')
                    or not isinstance(entry['key_id'],str) or not 1<=len(entry['key_id'].encode())<=256
                    or any(ord(c)<32 for c in entry['key_id'])):
                raise ValueError('RESTORE_KEY_ENTRY')
            scope=(entry['domain'],entry['key_id'])
            if previous is not None and scope<=previous:raise ValueError('RESTORE_KEY_ENTRY_ORDER')
            result[scope]=entry;previous=scope
        return result
    keys=catalogue(document(keys_raw,'restore-aes-keys/v1',1<<20),{'domain','key_id','key_b64'})
    witnesses=catalogue(document(witnesses_raw,'restore-aes-witnesses/v1',8<<20),
        {'domain','key_id','sealed_b64','aad_b64','plaintext_sha256'})
    decoded={scope:_decode(entry['key_b64'],32) for scope,entry in keys.items()}
    answers={scope:RestoreKeyWitness(_decode(entry['sealed_b64'],65536),
        _decode(entry['aad_b64'],4096),entry['plaintext_sha256']) for scope,entry in witnesses.items()}
    active()
    return AESGCMRestoreKeyProbe(decoded,answers,check_active=active)
