"""Bounded active-claim/COSE extraction; no cryptographic acceptance implied.

C2PA 2.4 sections Manifest Store, Claim and Claim Signature. Preserve original
CBOR bytes: reserializing a claim would change its signed representation.
"""
from dataclasses import dataclass
from struct import unpack_from


_SUFFIX=bytes.fromhex('00110010800000aa00389b71')


def type_uuid(fourcc):
    return fourcc+_SUFFIX


@dataclass(frozen=True)
class AssertionBytes:
    label: str
    type_uuid: bytes
    contents: bytes
    payloads: tuple[tuple[bytes,bytes],...]


@dataclass(frozen=True)
class ActiveClaimBytes:
    manifest_label: str
    claim_label: str
    claim: bytes
    cose_signature: bytes
    assertions: tuple[AssertionBytes,...] = ()


def extract_active_claim(store, *, check_active=lambda: None):
    if not isinstance(store,bytes) or not 32<=len(store)<=8<<20 or not callable(check_active):
        raise ValueError('SIGNER_JUMBF_INPUT')
    def guard():
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    guard()
    view=memoryview(store)
    count=0
    def boxes(start,end):
        nonlocal count
        result=[]
        while start<end:
            guard()
            count+=1
            if count>100000 or end-start<8:
                raise ValueError('SIGNER_JUMBF_BOX_LIMIT')
            size,kind=unpack_from('>I4s',view,start)
            header=8
            if size==1:
                if end-start<16:raise ValueError('SIGNER_JUMBF_SIZE')
                size=unpack_from('>Q',view,start+8)[0]
                header=16
            if size<header or size>end-start:
                raise ValueError('SIGNER_JUMBF_SIZE')
            result.append((kind,start+header,start+size))
            start+=size
        return result
    def describe(box):
        kind,start,end=box
        if kind!=b'jumb':raise ValueError('SIGNER_JUMBF_SUPERBOX')
        children=boxes(start,end)
        if not children or children[0][0]!=b'jumd':
            raise ValueError('SIGNER_JUMBF_DESCRIPTION')
        _,ds,de=children[0]
        if de-ds<18:raise ValueError('SIGNER_JUMBF_DESCRIPTION')
        uuid=bytes(view[ds:ds+16])
        flags=view[ds+16]
        if flags & ~31 or not flags & 2:
            raise ValueError('SIGNER_JUMBF_FLAGS')
        label_start=ds+17
        # Labels are bounded UTF-8, NUL terminated; optional fields may follow.
        stop=label_start
        while stop<de and view[stop]!=0 and stop-label_start<=1024:stop+=1
        if stop>=de or stop-label_start>1024 or stop==label_start:
            raise ValueError('SIGNER_JUMBF_LABEL')
        try:label=bytes(view[label_start:stop]).decode('utf-8')
        except UnicodeDecodeError as exc:raise ValueError('SIGNER_JUMBF_LABEL') from exc
        if any(ord(char)<32 for char in label):raise ValueError('SIGNER_JUMBF_LABEL')
        tail=stop+1+(4 if flags&4 else 0)+(32 if flags&8 else 0)
        if tail>de:raise ValueError('SIGNER_JUMBF_DESCRIPTION')
        if flags&16:
            private=boxes(tail,de)
            if len(private)!=1:raise ValueError('SIGNER_JUMBF_DESCRIPTION')
        elif tail!=de:
            raise ValueError('SIGNER_JUMBF_DESCRIPTION')
        return uuid,label,children[1:]
    roots=boxes(0,len(store))
    if len(roots)!=1:raise ValueError('SIGNER_JUMBF_STORE')
    uuid,label,manifests=describe(roots[0])
    if uuid!=type_uuid(b'c2pa') or label!='c2pa' or not manifests:
        raise ValueError('SIGNER_JUMBF_STORE')
    seen=set()
    active=None
    for manifest in manifests:
        uuid,label,children=describe(manifest)
        if uuid not in (type_uuid(b'c2ma'),type_uuid(b'c2um')) or label in seen:
            raise ValueError('SIGNER_JUMBF_MANIFEST')
        seen.add(label)
        active=(label,children)
    manifest_label,children=active
    claim,signature,claim_label=None,None,None
    labels=set()
    assertions=[]
    for child in children:
        uuid,label,payloads=describe(child)
        if label in labels:raise ValueError('SIGNER_JUMBF_DUPLICATE_LABEL')
        labels.add(label)
        if uuid==type_uuid(b'c2as') or label=='c2pa.assertions':
            if uuid!=type_uuid(b'c2as') or label!='c2pa.assertions' or not payloads:
                raise ValueError('SIGNER_JUMBF_ASSERTION_STORE')
            assertion_labels=set()
            for assertion in payloads:
                guard()
                auuid,alabel,apayloads=describe(assertion)
                if alabel in assertion_labels or len(assertions)>=4096 or not apayloads:
                    raise ValueError('SIGNER_JUMBF_ASSERTION')
                assertion_labels.add(alabel)
                _,astart,aend=assertion
                assertions.append(AssertionBytes(alabel,auuid,bytes(view[astart:aend]),
                    tuple((kind,bytes(view[start:end])) for kind,start,end in apayloads)))
        is_claim=uuid==type_uuid(b'c2cl') or label in ('c2pa.claim','c2pa.claim.v2')
        is_signature=uuid==type_uuid(b'c2cs') or label=='c2pa.signature'
        if not is_claim and not is_signature:continue
        if (len(payloads)!=1 or payloads[0][0]!=b'cbor'
                or payloads[0][1]==payloads[0][2]):
            raise ValueError('SIGNER_JUMBF_CBOR')
        _,start,end=payloads[0]
        if is_claim:
            if uuid!=type_uuid(b'c2cl') or label not in ('c2pa.claim','c2pa.claim.v2') or claim is not None:
                raise ValueError('SIGNER_JUMBF_CLAIM')
            claim,claim_label=bytes(view[start:end]),label
        if is_signature:
            if uuid!=type_uuid(b'c2cs') or label!='c2pa.signature' or signature is not None:
                raise ValueError('SIGNER_JUMBF_SIGNATURE')
            signature=bytes(view[start:end])
    if claim is None or signature is None:raise ValueError('SIGNER_JUMBF_CLAIM_SIGNATURE_MISSING')
    guard()
    return ActiveClaimBytes(manifest_label,claim_label,claim,signature,tuple(assertions))
