"""Bounded CBOR subset for C2PA claims/COSE (RFC8949), not a generic codec.

Definite lengths, finite numbers, integer/text/byte map keys, explicit tags.
Duplicate keys and trailing bytes are always errors. Original signed bytes
remain external to the decoded tree and must never be reconstructed from it.
"""
from dataclasses import dataclass
from math import isfinite
from struct import pack,unpack


@dataclass(frozen=True)
class CBORTag:
    number: int
    value: object


def decode_cbor(raw, *, maximum=8<<20, allowed_tags=(), deterministic=False, check_active=lambda: None):
    if (not isinstance(raw,bytes) or not 1<=len(raw)<=maximum<=8<<20
            or type(deterministic) is not bool or not callable(check_active)):
        raise ValueError('SIGNER_CBOR_INPUT')
    position,nodes=0,0
    tags=frozenset(allowed_tags)
    def guard():
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    guard()
    def take(size):
        nonlocal position
        if size<0 or size>len(raw)-position:raise ValueError('SIGNER_CBOR_TRUNCATED')
        start=position
        position+=size
        return raw[start:position]
    def item(depth):
        nonlocal nodes
        guard()
        nodes+=1
        if nodes>100000 or depth>64:raise ValueError('SIGNER_CBOR_COMPLEXITY')
        initial=take(1)[0]
        major,additional=initial>>5,initial&31
        if major==7:
            if additional in (20,21,22):return {20:False,21:True,22:None}[additional]
            if additional in (25,26,27):
                width,fmt={25:(2,'>e'),26:(4,'>f'),27:(8,'>d')}[additional]
                value=unpack(fmt,take(width))[0]
                if not isfinite(value):raise ValueError('SIGNER_CBOR_NONFINITE')
                if deterministic:
                    # Core deterministic encoding requires the shortest float
                    # width preserving the value (including negative zero).
                    for shorter in (('>e',) if width==4 else ('>e','>f') if width==8 else ()):
                        try:roundtrip=unpack(shorter,pack(shorter,value))[0]
                        except (OverflowError,ValueError):continue
                        if pack('>d',roundtrip)==pack('>d',value):
                            raise ValueError('SIGNER_CBOR_NONPREFERRED_FLOAT')
                return value
            raise ValueError('SIGNER_CBOR_SIMPLE')
        if additional<24:value=additional
        elif additional in (24,25,26,27):
            width={24:1,25:2,26:4,27:8}[additional]
            value=int.from_bytes(take(width),'big')
            # Preferred shortest integer/length/tag representation.
            if value<({1:24,2:256,4:65536,8:1<<32}[width]):
                raise ValueError('SIGNER_CBOR_NONPREFERRED')
        else:raise ValueError('SIGNER_CBOR_INDEFINITE_OR_RESERVED')
        if major==0:return value
        if major==1:return -1-value
        if major in (2,3):
            data=take(value)
            if major==2:return data
            try:return data.decode('utf-8')
            except UnicodeDecodeError as exc:raise ValueError('SIGNER_CBOR_UTF8') from exc
        if major in (4,5):
            if value>100000-nodes:raise ValueError('SIGNER_CBOR_COMPLEXITY')
            if major==4:return [item(depth+1) for _ in range(value)]
            result={}
            previous=None
            for _ in range(value):
                key_start=position
                key=item(depth+1)
                encoded_key=raw[key_start:position]
                if type(key) not in (int,str,bytes):raise ValueError('SIGNER_CBOR_MAP_KEY')
                if key in result:raise ValueError('SIGNER_CBOR_DUPLICATE_KEY')
                # RFC8949 core order is bytewise lexicographic order of encoded
                # keys, not Python value order or legacy length-first sorting.
                if deterministic and previous is not None and encoded_key<=previous:
                    raise ValueError('SIGNER_CBOR_MAP_ORDER')
                previous=encoded_key
                result[key]=item(depth+1)
            return result
        if major==6:
            if value not in tags:raise ValueError('SIGNER_CBOR_TAG')
            return CBORTag(value,item(depth+1))
        raise ValueError('SIGNER_CBOR_TYPE')
    decoded=item(0)
    if position!=len(raw):raise ValueError('SIGNER_CBOR_TRAILING')
    guard()
    return decoded


def encode_cbor_bytes(value):
    """Encode only a byte string, for constructing COSE Sig_structure."""
    if not isinstance(value,bytes) or len(value)>8<<20:
        raise ValueError('SIGNER_CBOR_BYTES')
    size=len(value)
    if size<24:head=bytes([0x40+size])
    elif size<256:head=b'\x58'+size.to_bytes(1,'big')
    elif size<65536:head=b'\x59'+size.to_bytes(2,'big')
    else:head=b'\x5a'+size.to_bytes(4,'big')
    return head+value
