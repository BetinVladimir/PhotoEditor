"""Declared input-only byte access for independent Render reconstruction."""
from hashlib import sha256
from .adoption import AdoptionInvalid, MAX_JSON_BYTES


class RenderReplayReader:
    """Prevent accidental use of submitted output bytes as a replay oracle.

    This is an API boundary for trusted installed providers, not a sandbox for
    arbitrary Python code. The underlying reader proves exact task/attempt
    declaration and immutable object versions. Full iteration additionally
    verifies exact size/hash; early close aborts rather than proves a stream.
    """
    def __init__(self, reader, check_active=lambda: None):
        self.__reader=reader
        self.__check=check_active

    def evidence(self, ref):
        self.__check()
        evidence=self.__reader.evidence(ref)
        if (evidence.ref!=ref or evidence.source!='input'
                or type(evidence.bytes) is not int or evidence.bytes<1):
            raise AdoptionInvalid('RENDER_REPLAY_INPUT_REQUIRED')
        return evidence

    def staged_outputs(self):
        raise AdoptionInvalid('RENDER_REPLAY_INPUT_REQUIRED')

    def read_json(self, ref, max_bytes):
        if type(max_bytes) is not int or not 1<=max_bytes<=MAX_JSON_BYTES:
            raise AdoptionInvalid('RENDER_REPLAY_BOUND_INVALID')
        evidence=self.evidence(ref)
        if evidence.media_type!='application/json' or evidence.bytes>max_bytes:
            raise AdoptionInvalid('RENDER_REPLAY_BOUND_INVALID')
        raw=self.__reader.read_json(ref,max_bytes)
        self.__check()
        if (not isinstance(raw,bytes) or len(raw)!=evidence.bytes
                or sha256(raw).hexdigest()!=ref.sha256):
            raise AdoptionInvalid('RENDER_REPLAY_BYTES_INVALID')
        return raw

    def iter_bytes(self, ref, max_bytes):
        if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1:
            raise AdoptionInvalid('RENDER_REPLAY_BOUND_INVALID')
        evidence=self.evidence(ref)
        if evidence.bytes>max_bytes:
            raise AdoptionInvalid('RENDER_REPLAY_BOUND_INVALID')
        count=0;digest=sha256()
        iterator=iter(self.__reader.iter_bytes(ref,evidence.bytes))
        try:
            for block in iterator:
                self.__check()
                if (not isinstance(block,bytes) or not 1<=len(block)<=65536
                        or count+len(block)>evidence.bytes):
                    raise AdoptionInvalid('RENDER_REPLAY_BYTES_INVALID')
                count+=len(block);digest.update(block)
                yield block
            self.__check()
            if count!=evidence.bytes or digest.hexdigest()!=ref.sha256:
                raise AdoptionInvalid('RENDER_REPLAY_BYTES_INVALID')
        finally:
            close=getattr(iterator,'close',None)
            if close is not None:close()
