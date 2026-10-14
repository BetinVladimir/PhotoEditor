import base64
from hashlib import sha256
import os
import pytest
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_key_installation import load_restore_key_probe
import photo_worker.runtime.restore_key_installation as key_installation


@pytest.mark.parametrize('mode',['valid','bad_pin','public_key_file','symlink','hardlink','extra_field','bad_base64','mismatched_scope'])
def test_private_key_installation_requires_pinned_closed_witness(tmp_path,mode):
    key=b'k'*32;nonce=b'n'*12;plain=b'historical witness'
    b64=lambda b:base64.b64encode(b).decode()
    entry={'domain':'mail','key_id':'v1','key_b64':b64(key)}
    witness={'domain':'mail','key_id':'v1','sealed_b64':b64(nonce+AESGCM(key).encrypt(nonce,plain,b'')),
        'aad_b64':'','plaintext_sha256':sha256(plain).hexdigest()}
    if mode=='extra_field':entry['unexpected']='forbidden'
    if mode=='bad_base64':entry['key_b64']='!'
    if mode=='mismatched_scope':witness['key_id']='v2'
    keys=tmp_path/'keys.json';answers=tmp_path/'answers.json'
    keys.write_bytes(canonical_bytes({'schema_version':'restore-aes-keys/v1','entries':[entry]}))
    answers.write_bytes(canonical_bytes({'schema_version':'restore-aes-witnesses/v1','entries':[witness]}))
    keys.chmod(0o600);answers.chmod(0o600)
    pin=sha256(answers.read_bytes()).hexdigest()
    if mode=='bad_pin':pin='0'*64
    if mode=='public_key_file':keys.chmod(0o644)
    if mode=='hardlink':os.link(keys,tmp_path/'alias')
    if mode=='symlink':
        alias=tmp_path/'alias';alias.symlink_to(keys);keys=alias
    def run():return load_restore_key_probe(keys,answers,pin,check_active=lambda:None)
    if mode=='valid':assert run()('mail','v1') is True
    else:
        with pytest.raises(ValueError):run()


def test_private_reader_refuses_replaced_ancestor_before_open(tmp_path,monkeypatch):
    parent=tmp_path/'private';parent.mkdir(mode=0o700)
    path=parent/'receipt.json';path.write_bytes(b'private-receipt');path.chmod(0o600)
    moved=tmp_path/'moved'
    original=key_installation._open_private_descriptor
    def swapped(target,active):
        parent.rename(moved)
        parent.symlink_to(moved,target_is_directory=True)
        return original(target,active)
    monkeypatch.setattr(key_installation,'_open_private_descriptor',swapped)
    with pytest.raises(OSError):
        key_installation._read_private(path,1024,lambda:None)
    assert (moved/'receipt.json').read_bytes()==b'private-receipt'


def test_private_reader_refuses_explicit_false_before_open(tmp_path,monkeypatch):
    path=tmp_path/'receipt.json';path.write_bytes(b'private-receipt');path.chmod(0o600)
    monkeypatch.setattr(key_installation,'_open_private_descriptor',
        lambda *args:pytest.fail('private file opened after cancellation'))
    with pytest.raises(RuntimeError,match='RESTORE_PRIVATE_READ_INACTIVE'):
        key_installation._read_private(path,1024,lambda:False)


def test_key_installation_refuses_false_after_private_reads(tmp_path,monkeypatch):
    import base64
    key=b'k'*32;nonce=b'n'*12;plain=b'historical witness'
    sealed=nonce+AESGCM(key).encrypt(nonce,plain,b'')
    keys=canonical_bytes({'schema_version':'restore-aes-keys/v1','entries':[
        {'domain':'mail','key_id':'v1','key_b64':base64.b64encode(key).decode()}]})
    witnesses=canonical_bytes({'schema_version':'restore-aes-witnesses/v1','entries':[
        {'domain':'mail','key_id':'v1','sealed_b64':base64.b64encode(sealed).decode(),
         'aad_b64':'','plaintext_sha256':sha256(plain).hexdigest()}]})
    allowed=True;reads=0
    def private_read(*args):
        nonlocal allowed,reads
        reads+=1
        if reads==2:allowed=False
        return keys if reads==1 else witnesses
    monkeypatch.setattr(key_installation,'_read_private',private_read)
    with pytest.raises(RuntimeError,match='RESTORE_KEY_INSTALLATION_INACTIVE'):
        load_restore_key_probe(tmp_path/'keys',tmp_path/'witnesses',
            sha256(witnesses).hexdigest(),check_active=lambda:allowed)
    assert reads==2
