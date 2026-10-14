"""C2PA claim envelope checks; SDK retains full reference/metadata semantics."""
import re


def verify_claim_structure(extracted, claim, *, mime=None):
    if not isinstance(claim, dict):
        raise ValueError('SIGNER_CLAIM_STRUCTURE')

    def text(value):
        return isinstance(value, str) and 0 < len(value) <= 65536

    if not text(claim.get('instanceID')) or not text(claim.get('signature')):
        raise ValueError('SIGNER_CLAIM_REQUIRED_TEXT')
    common = {'instanceID', 'claim_generator_info', 'signature', 'dc:title',
              'redacted_assertions', 'alg', 'alg_soft', 'metadata'}
    info = claim.get('claim_generator_info')
    if extracted.claim_label == 'c2pa.claim.v2':
        if any(key in claim for key in ('claim_generator', 'assertions', 'dc:format')):
            raise ValueError('SIGNER_CLAIM_VERSION')
        allowed = common | {'created_assertions', 'gathered_assertions', 'specVersion'}
        generators = [info]
        required, optional = 'created_assertions', 'gathered_assertions'
    elif extracted.claim_label == 'c2pa.claim':
        if any(key in claim for key in ('created_assertions', 'gathered_assertions')):
            raise ValueError('SIGNER_CLAIM_VERSION')
        if not text(claim.get('claim_generator')) or not text(claim.get('dc:format')):
            raise ValueError('SIGNER_CLAIM_REQUIRED_TEXT')
        if mime is not None and claim['dc:format'] != mime:
            raise ValueError('SIGNER_CLAIM_MIME')
        if not isinstance(info, list) or not 1 <= len(info) <= 4096:
            raise ValueError('SIGNER_CLAIM_GENERATOR')
        allowed = common | {'claim_generator', 'assertions', 'dc:format'}
        generators = info
        required, optional = 'assertions', None
    else:
        raise ValueError('SIGNER_CLAIM_VERSION')
    if set(claim) - allowed:
        raise ValueError('SIGNER_CLAIM_UNKNOWN_FIELD')
    if 'metadata' in claim and not isinstance(claim['metadata'], dict):
        raise ValueError('SIGNER_CLAIM_METADATA')
    for generator in generators:
        if not isinstance(generator, dict) or not text(generator.get('name')):
            raise ValueError('SIGNER_CLAIM_GENERATOR')
        # specVersion is an informational standard extension; other extras
        # use the entity-specific dotted namespace defined by C2PA.
        known = {'name', 'version', 'icon', 'operating_system', 'specVersion'}
        if any(not isinstance(key, str) or (key not in known and
               re.fullmatch(r'([a-zA-Z0-9_-]+\.)+[a-zA-Z0-9_-]+', key) is None)
               for key in generator):
            raise ValueError('SIGNER_CLAIM_GENERATOR_NAMESPACE')
        for key in ('version', 'operating_system'):
            if key in generator and not isinstance(generator[key], str):
                raise ValueError('SIGNER_CLAIM_GENERATOR')
        if 'specVersion' in generator and not text(generator['specVersion']):
            raise ValueError('SIGNER_CLAIM_GENERATOR')
        if 'icon' in generator and not isinstance(generator['icon'], dict):
            raise ValueError('SIGNER_CLAIM_GENERATOR_ICON')
    entries = claim.get(required)
    if not isinstance(entries, list) or not 1 <= len(entries) <= 4096:
        raise ValueError('SIGNER_CLAIM_ASSERTIONS')
    if optional in claim:
        entries = claim[optional]
        if not isinstance(entries, list) or not 1 <= len(entries) <= 4096:
            raise ValueError('SIGNER_CLAIM_ASSERTIONS')
    for key in ('dc:title', 'alg', 'alg_soft', 'specVersion'):
        if key in claim and not text(claim[key]):
            raise ValueError('SIGNER_CLAIM_OPTIONAL_TEXT')
    if 'redacted_assertions' in claim:
        entries = claim['redacted_assertions']
        if not isinstance(entries, list) or not 1 <= len(entries) <= 4096 or not all(map(text, entries)):
            raise ValueError('SIGNER_CLAIM_REDACTIONS')
