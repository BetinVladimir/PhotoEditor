"""Operator-installed AES-256-GCM known-answer key recovery probe.

Witnesses must be independently pinned from before key recovery, never generated
with the candidate restored key. Matching a newly self-generated witness cannot
prove correct historical material. No secrets are included in repr or errors.
"""
from dataclasses import dataclass,field
from hashlib import sha256
import re


@dataclass(frozen=True)
class RestoreKeyWitness:
    sealed: bytes=field(repr=False)
    aad: bytes=field(repr=False)
    plaintext_sha256: str


class AESGCMRestoreKeyProbe:
    def __init__(self,keys,witnesses,*,check_active):
        if (not isinstance(keys,dict) or not isinstance(witnesses,dict)
                or not keys or len(keys)>4096 or set(keys)!=set(witnesses)
                or not callable(check_active)):
            raise ValueError('RESTORE_AES_INSTALLATION')
        def active():
            if check_active() is False:
                raise RuntimeError('RESTORE_AES_INACTIVE')
        self._keys={};self._witnesses={};self.active=active
        for scope,key in keys.items():
            active()
            if (not isinstance(scope,tuple) or len(scope)!=2 or scope[0] not in ('mail','capture')
                    or not isinstance(scope[1],str) or not 1<=len(scope[1].encode())<=256
                    or not isinstance(key,bytes) or len(key)!=32):
                raise ValueError('RESTORE_AES_INSTALLATION')
            witness=witnesses[scope]
            if (not isinstance(witness,RestoreKeyWitness)
                    or not isinstance(witness.sealed,bytes) or not 28<=len(witness.sealed)<=65536
                    or not isinstance(witness.aad,bytes) or len(witness.aad)>4096
                    or not isinstance(witness.plaintext_sha256,str)
                    or re.fullmatch('[0-9a-f]{64}',witness.plaintext_sha256) is None):
                raise ValueError('RESTORE_AES_WITNESS')
            self._keys[scope]=bytes(key)
            self._witnesses[scope]=witness
        active()

    def __call__(self,domain,key_id):
        self.active();scope=(domain,key_id)
        if scope not in self._keys:raise ValueError('RESTORE_AES_KEY_UNAVAILABLE')
        from cryptography.hazmat.primitives.ciphers.aead import AESGCM
        witness=self._witnesses[scope]
        try:
            plain=AESGCM(self._keys[scope]).decrypt(witness.sealed[:12],
                witness.sealed[12:],witness.aad)
        except Exception:
            raise ValueError('RESTORE_AES_PROBE_FAILED') from None
        matched=sha256(plain).hexdigest()==witness.plaintext_sha256
        del plain
        self.active()
        if not matched:raise ValueError('RESTORE_AES_PROBE_FAILED')
        return True
