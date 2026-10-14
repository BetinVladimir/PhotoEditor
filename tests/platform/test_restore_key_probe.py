from hashlib import sha256
import pytest
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from photo_worker.runtime.restore_key_probe import AESGCMRestoreKeyProbe,RestoreKeyWitness


@pytest.mark.parametrize('mode',['valid','wrong_key','wrong_aad','tampered','wrong_plain_hash','missing','cancelled'])
def test_restored_key_requires_authenticated_independent_witness(mode):
    key=b'k'*32;nonce=b'n'*12;plain=b'historical recovery challenge';aad=b'pinned operator witness'
    ciphertext=AESGCM(key).encrypt(nonce,plain,aad)
    if mode=='tampered':ciphertext=ciphertext[:-1]+bytes([ciphertext[-1]^1])
    witness=RestoreKeyWitness(nonce+ciphertext,b'wrong' if mode=='wrong_aad' else aad,
        '0'*64 if mode=='wrong_plain_hash' else sha256(plain).hexdigest())
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if mode=='cancelled' and calls>=3:raise RuntimeError('cancelled')
    keys={('mail','v1'):b'x'*32 if mode=='wrong_key' else key}
    probe=AESGCMRestoreKeyProbe(keys,{('mail','v1'):witness},check_active=active)
    keys.clear()  # Installed probe owns its catalogue.
    if mode=='valid':assert probe('mail','v1') is True
    else:
        with pytest.raises((ValueError,RuntimeError)):probe('mail','v2' if mode=='missing' else 'v1')
    assert plain.decode() not in repr(witness) and aad.decode() not in repr(witness)


@pytest.mark.parametrize('after_decrypt', [False, True])
def test_restored_key_refuses_explicit_false_activity(after_decrypt):
    key=b'k'*32;nonce=b'n'*12;plain=b'historical recovery challenge'
    witness=RestoreKeyWitness(nonce+AESGCM(key).encrypt(nonce,plain,b''),
        b'',sha256(plain).hexdigest())
    calls=0
    def active():
        nonlocal calls
        calls+=1
        return calls<4 if after_decrypt else calls<3
    probe=AESGCMRestoreKeyProbe({('mail','v1'):key},{('mail','v1'):witness},
        check_active=active)
    with pytest.raises(RuntimeError,match='RESTORE_AES_INACTIVE'):
        probe('mail','v1')
