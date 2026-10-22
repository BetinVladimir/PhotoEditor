"""Deterministic catalog projection; does not grant deployment qualification."""
from copy import deepcopy


def project_retouch_preview_catalog(catalog):
    result = deepcopy(catalog)
    operations = result.get('operations')
    if not isinstance(operations,list):
        raise ValueError('SEMANTIC_RETOUCH_CATALOG_INVALID')
    sources = [op for op in operations if op.get('id') == 'semantic.preview_background_cleanup']
    existing = [op for op in operations if op.get('id') == 'semantic.preview_retouch_region']
    if len(sources) != 1 or len(existing) > 1:
        raise ValueError('SEMANTIC_RETOUCH_CATALOG_INVALID')
    operation = deepcopy(sources[0])
    operation.update(id='semantic.preview_retouch_region',params='SemanticRetouchPreviewInput',
        output='RetouchPreviewPresentation',envelope_schema='RetouchPreviewPresentation',
        resource_class='cpu',execution_pool='cpu',
        nested_outputs='Own full-face BeautyFilterV1 proposal with continuous transferred mask, exact protected/outside/alpha preservation, numerical evidence and four PNG sidecars. Photographer chooses result; no approval granted.')
    roles = operation.get('role_policies',{})
    if (operation.get('version') != '1' or operation.get('max_output_count') != 8
            or set(roles) != {'pixels','working_image','preview_evidence','preview_presentation','before','after','affected','protected'}):
        raise ValueError('SEMANTIC_RETOUCH_CATALOG_INVALID')
    roles['preview_evidence']['contract'] = 'RetouchPreviewNumericalEvidence'
    roles['preview_presentation']['contract'] = 'RetouchPreviewPresentation'
    if existing:
        operations[operations.index(existing[0])] = operation
    else:
        operations.append(operation)
    return result
