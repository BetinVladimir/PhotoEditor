"""Validate Reader output from qualified C2PA SDK; never trust task JSON reports.

Status codes follow contentauth/c2pa-rs validation_results and C2PA spec.
Expected signing certificate/key binding remains an independent operator check.
"""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from .cancellation import check_signing_active


def _bounded_sdk_tree(value, check_active=lambda: None):
    """Bound SDK-owned JSON shape before recursive canonical serialization."""
    stack = [(value, 1)]
    def guard():
        check_signing_active()
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    count = 0
    while stack:
        guard()
        node, depth = stack.pop()
        count += 1
        if count > 100000 or depth > 32:
            raise ValueError('SIGNER_VALIDATION_LIMIT')
        if isinstance(node, dict):
            stack.extend((child, depth + 1) for child in node.values())
        elif isinstance(node, list):
            stack.extend((child, depth + 1) for child in node)


def verify_sdk_reader(reader, policy, lineage, mime, *, check_active=lambda: None):
    if not callable(check_active):
        raise ValueError('SIGNER_VALIDATION_CANCELLATION')
    def guard():
        check_signing_active()
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    guard()
    if reader.is_embedded() is not True or reader.get_validation_state() != 'Trusted':
        raise ValueError('SIGNER_MANIFEST_UNTRUSTED')
    guard()
    results = reader.get_validation_results()
    guard()
    if not isinstance(results, dict):
        raise ValueError('SIGNER_VALIDATION_MISSING')
    _bounded_sdk_tree(results, guard)
    raw = canonical_bytes(results)
    if len(raw) > 8 << 20:
        raise ValueError('SIGNER_VALIDATION_LIMIT')
    active = results.get('activeManifest')
    if not isinstance(active, dict) or active.get('failure') != []:
        raise ValueError('SIGNER_ACTIVE_FAILURE')
    success = active.get('success')
    informational = active.get('informational')
    if (not isinstance(success, list) or not isinstance(informational, list)
            or any(not isinstance(item, dict) or not isinstance(item.get('code'), str)
                   for item in success + informational)):
        raise ValueError('SIGNER_VALIDATION_SHAPE')
    codes = {item['code'] for item in success}
    required = {'claimSignature.validated', 'claimSignature.insideValidity',
                'signingCredential.trusted'}
    if policy['timestamp_mode'] == 'required':
        required.update(('timeStamp.validated', 'timeStamp.trusted'))
    if policy['revocation_mode'] == 'required':
        required.add('signingCredential.ocsp.notRevoked')
    if (not required <= codes
            or not codes.intersection(('assertion.dataHash.match', 'assertion.boxesHash.match'))
            or any(item['code'].endswith('additionalExclusionsPresent')
                   for item in success + informational)):
        raise ValueError('SIGNER_BINDING_OR_POLICY_STATUS')
    # Ingredient failures must not be hidden by success on the active manifest.
    ingredients = results.get('ingredientDeltas', [])
    if not isinstance(ingredients, (dict, list)):
        raise ValueError('SIGNER_INGREDIENT_SHAPE')
    stack = [ingredients]
    count = 0
    while stack:
        guard()
        item = stack.pop()
        count += 1
        if count > 100000:
            raise ValueError('SIGNER_VALIDATION_LIMIT')
        if isinstance(item, dict):
            if 'failure' in item:
                if not isinstance(item['failure'], list):
                    raise ValueError('SIGNER_INGREDIENT_SHAPE')
                if item['failure']:
                    raise ValueError('SIGNER_INGREDIENT_FAILURE')
            stack.extend(item.values())
        elif isinstance(item, list):
            stack.extend(item)
    guard()
    manifest = reader.get_active_manifest()
    guard()
    if not isinstance(manifest, dict) or manifest.get('format') != mime:
        raise ValueError('SIGNER_ACTIVE_MANIFEST')
    _bounded_sdk_tree(manifest, guard)
    if len(canonical_bytes(manifest)) > 8 << 20:
        raise ValueError('SIGNER_VALIDATION_LIMIT')
    guard()
    assertions = manifest.get('assertions')
    if not isinstance(assertions, list):
        raise ValueError('SIGNER_ASSERTIONS')
    matches = [item for item in assertions if isinstance(item, dict)
               and item.get('label') == 'org.photoeditor.lineage']
    if len(matches) != 1 or canonical_bytes(matches[0].get('data')) != canonical_bytes(lineage):
        raise ValueError('SIGNER_LINEAGE_MISMATCH')
    guard()
    return sha256(raw).hexdigest()
