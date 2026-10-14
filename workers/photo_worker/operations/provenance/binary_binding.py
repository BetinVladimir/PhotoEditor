"""Join physical JUMBF/COSE identity with the SDK's trusted active manifest.

Container-store equality is separately mandatory in sdk.py/verify_files.py.
This join is not a replacement for native installation/chain trust authority.
"""
from hashlib import sha256

from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.provenance import SigningPolicy,SigningLineage
from .cbor import decode_cbor
from .cose_claim import verify_claim_signature
from .jumbf_claim import extract_active_claim
from .verify import verify_sdk_reader
from .assertion_binding import verify_assertion_bindings
from .claim_structure import verify_claim_structure


def verify_binary_binding(reader,policy,lineage,store,certificate_chain,algorithm,*,check_active=lambda:None):
    if (not isinstance(policy,SigningPolicy) or not isinstance(lineage,SigningLineage)
            or not isinstance(certificate_chain,bytes) or not 1<=len(certificate_chain)<=1<<20
            or sha256(certificate_chain).hexdigest()!=policy.certificate_chain_sha
            or not callable(check_active)):
        raise ValueError('SIGNER_BINARY_POLICY')
    def guard():
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    guard()
    extracted=extract_active_claim(store,check_active=guard)
    claim=decode_cbor(extracted.claim,deterministic=True,check_active=guard)
    verify_claim_structure(extracted,claim)
    # Both canonical relative and absolute URIs resolve to the signature box
    # in this exact active manifest; no cross-manifest/external alias accepted.
    if (not isinstance(claim,dict) or claim.get('signature') not in (
            'self#jumbf=c2pa.signature',
            'self#jumbf=/c2pa/'+extracted.manifest_label+'/c2pa.signature')):
        raise ValueError('SIGNER_BINARY_SIGNATURE_URI')
    verified=verify_claim_signature(extracted.claim,extracted.cose_signature,
        certificate_chain,check_active=guard)
    if verified.algorithm!=algorithm:raise ValueError('SIGNER_BINARY_ALGORITHM')
    verify_assertion_bindings(extracted,claim,lineage,check_active=guard)
    guard()
    raw=reader.json()
    guard()
    document=parse_json(raw,max_bytes=8<<20)
    if (not isinstance(document,dict) or document.get('active_manifest')!=extracted.manifest_label
            or not isinstance(document.get('manifests'),dict)):
        raise ValueError('SIGNER_BINARY_ACTIVE_MANIFEST')
    active=document['manifests'].get(extracted.manifest_label)
    guard()
    manifest=reader.get_active_manifest()
    guard()
    if not isinstance(active,dict) or canonical_bytes(active)!=canonical_bytes(manifest):
        raise ValueError('SIGNER_BINARY_ACTIVE_MANIFEST')
    mime=active.get('format')
    if mime not in policy.allowed_mime:raise ValueError('SIGNER_BINARY_MIME')
    verify_claim_structure(extracted,claim,mime=mime)
    # SDK must prove hard binding, trusted credential/timestamp/OCSP according
    # to policy and exact custom lineage assertion on this same active manifest.
    verify_sdk_reader(reader,policy.model_dump(mode='json'),lineage.model_dump(mode='json'),mime,
        check_active=guard)
    guard()
    current_document=reader.json()
    guard()
    current_manifest=reader.get_active_manifest()
    guard()
    if (canonical_bytes(parse_json(current_document,max_bytes=8<<20))!=canonical_bytes(document)
            or canonical_bytes(current_manifest)!=canonical_bytes(active)):
        raise ValueError('SIGNER_BINARY_READER_CHANGED')
    guard()
    return verified
