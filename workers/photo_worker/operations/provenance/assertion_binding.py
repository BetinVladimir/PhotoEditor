"""Replay active claim assertion hashes and exact PhotoEditor lineage bytes."""
from hashlib import sha256,sha384,sha512
from hmac import compare_digest

from photo_worker.contracts.base import canonical_bytes,parse_json
from .cbor import decode_cbor
from .jumbf_claim import type_uuid


def verify_assertion_bindings(extracted,claim,lineage,*,check_active=lambda:None):
    if not callable(check_active):raise ValueError('SIGNER_ASSERTION_CANCELLATION')
    def guard():
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    guard()
    if not isinstance(claim,dict):raise ValueError('SIGNER_ASSERTION_CLAIM')
    if extracted.claim_label=='c2pa.claim.v2':
        if 'assertions' in claim:raise ValueError('SIGNER_ASSERTION_CLAIM_VERSION')
        fields=('created_assertions','gathered_assertions')
        if not claim.get('created_assertions'):raise ValueError('SIGNER_ASSERTION_CREATED_MISSING')
    else:
        if 'created_assertions' in claim or 'gathered_assertions' in claim:
            raise ValueError('SIGNER_ASSERTION_CLAIM_VERSION')
        fields=('assertions',)
    inventory={item.label:item for item in extracted.assertions}
    if len(inventory)!=len(extracted.assertions) or not inventory:
        raise ValueError('SIGNER_ASSERTION_INVENTORY')
    references={}
    prefix='self#jumbf=c2pa.assertions/'
    absolute='self#jumbf=/c2pa/'+extracted.manifest_label+'/c2pa.assertions/'
    for field in fields:
        entries=claim.get(field,[])
        if not isinstance(entries,list) or len(entries)>4096:
            raise ValueError('SIGNER_ASSERTION_REFERENCES')
        for entry in entries:
            guard()
            if not isinstance(entry,dict) or not isinstance(entry.get('url'),str):
                raise ValueError('SIGNER_ASSERTION_URI')
            uri=entry['url']
            if uri.startswith(prefix):label=uri[len(prefix):]
            elif uri.startswith(absolute):label=uri[len(absolute):]
            else:raise ValueError('SIGNER_ASSERTION_URI')
            if not label or any(char in label for char in '/%?#\\') or label in references or label not in inventory:
                raise ValueError('SIGNER_ASSERTION_REFERENCE')
            algorithm=entry.get('alg',claim.get('alg'))
            hash_function={'sha256':sha256,'sha384':sha384,'sha512':sha512}.get(algorithm) if isinstance(algorithm,str) else None
            digest=entry.get('hash')
            if hash_function is None or not isinstance(digest,bytes) or not compare_digest(
                    hash_function(inventory[label].contents).digest(),digest):
                raise ValueError('SIGNER_ASSERTION_HASH')
            references[label]=entry
    if set(references)!=set(inventory):raise ValueError('SIGNER_ASSERTION_UNDECLARED')
    witness=inventory.get('org.photoeditor.lineage')
    if witness is None:raise ValueError('SIGNER_ASSERTION_LINEAGE_MISSING')
    payloads=[item for item in witness.payloads if item[0]!=b'free']
    if len(payloads)!=1:raise ValueError('SIGNER_ASSERTION_LINEAGE_PAYLOAD')
    kind,data=payloads[0]
    if kind==b'json' and witness.type_uuid==type_uuid(b'json'):
        decoded=parse_json(data,max_bytes=8<<20)
    elif kind==b'cbor' and witness.type_uuid==type_uuid(b'cbor'):
        decoded=decode_cbor(data,check_active=guard)
    else:raise ValueError('SIGNER_ASSERTION_LINEAGE_TYPE')
    if canonical_bytes(decoded)!=canonical_bytes(lineage):
        raise ValueError('SIGNER_ASSERTION_LINEAGE_BINDING')
    guard()
