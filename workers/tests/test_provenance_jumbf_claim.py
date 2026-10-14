"""Structural extraction fixtures; arbitrary CBOR bytes are not valid claims."""
from struct import pack

import pytest

from photo_worker.operations.provenance.jumbf_claim import extract_active_claim,type_uuid


def box(kind,payload):return pack('>I',len(payload)+8)+kind+payload


def superbox(kind,label,children):
    return box(b'jumb',box(b'jumd',type_uuid(kind)+b'\x03'+label.encode()+b'\0')+b''.join(children))


def manifest(label='urn:uuid:active',claim_label='c2pa.claim.v2'):
    return superbox(b'c2ma',label,[
        superbox(b'c2cl',claim_label,[box(b'cbor',b'claim-original-bytes')]),
        superbox(b'c2cs','c2pa.signature',[box(b'cbor',b'cose-original-bytes')])])


@pytest.mark.parametrize('claim_label',['c2pa.claim','c2pa.claim.v2'])
def test_original_active_claim_bytes_preserved(claim_label):
    raw=superbox(b'c2pa','c2pa',[manifest('previous'),manifest(claim_label=claim_label)])
    extracted=extract_active_claim(raw)
    assert extracted.manifest_label=='urn:uuid:active'
    assert extracted.claim_label==claim_label
    assert extracted.claim==b'claim-original-bytes'
    assert extracted.cose_signature==b'cose-original-bytes'


@pytest.mark.parametrize('mode',['truncated','trailing','duplicate_manifest','duplicate_claim','missing_signature','wrong_claim_uuid','wrong_store'])
def test_ambiguous_or_malformed_structure_refused(mode):
    child=manifest()
    raw=superbox(b'c2pa','c2pa',[child])
    if mode=='truncated':raw=raw[:-1]
    if mode=='trailing':raw+=b'x'
    if mode=='duplicate_manifest':raw=superbox(b'c2pa','c2pa',[child,child])
    if mode in ('duplicate_claim','missing_signature','wrong_claim_uuid'):
        claim=superbox(b'c2cl' if mode!='wrong_claim_uuid' else b'xxxx','c2pa.claim.v2',[box(b'cbor',b'claim')])
        children=[claim,claim] if mode=='duplicate_claim' else [claim]
        raw=superbox(b'c2pa','c2pa',[superbox(b'c2ma','active',children)])
    if mode=='wrong_store':raw=superbox(b'xxxx','c2pa',[child])
    with pytest.raises(ValueError):extract_active_claim(raw)


def test_cancellation_propagates():
    def active():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):extract_active_claim(superbox(b'c2pa','c2pa',[manifest()]),check_active=active)


def test_boolean_denial_refuses_jumbf_before_structure_and_at_return():
    raw=superbox(b'c2pa','c2pa',[manifest()])
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        extract_active_claim(raw,check_active=lambda:False)
    calls=0
    def active():
        nonlocal calls
        calls+=1
        return calls<100
    # A short valid structure has fewer than 100 checks; withdraw at the
    # independently observed final boundary.
    extract_active_claim(raw,check_active=active)
    final=calls
    calls=0
    def deny_final():
        nonlocal calls
        calls+=1
        return calls<final
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        extract_active_claim(raw,check_active=deny_final)


def test_extended_root_length_supported():
    raw=superbox(b'c2pa','c2pa',[manifest()])
    extended=pack('>I4sQ',1,b'jumb',len(raw)+8)+raw[8:]
    assert extract_active_claim(extended).claim==b'claim-original-bytes'
