"""Project current endpoint registry onto OpenAPI without rebuilding other contracts."""
from pathlib import Path
import copy
import json
import re

ROOT = Path(__file__).resolve().parents[1]


def synchronize(document, endpoints):
    result = copy.deepcopy(document)
    paths = result.setdefault('paths', {})
    for endpoint in endpoints:
        path, method = endpoint['path'], endpoint['method'].lower()
        if endpoint['role'] == 'capability':
            continue  # Token routes retain their specialized media contracts.
        operation = paths.setdefault(path, {}).setdefault(method, {})
        operation.update({'operationId': endpoint['handler'], 'x-role': endpoint['role'], 'x-package': endpoint['package']})
        parameters = operation.setdefault('parameters', [])
        if path.endswith('/legacy-imports/reconcile'):
            operation['security'] = [{'userBearer': []}]
            operation['x-web-mutation-csrf'] = True
            operation['description'] = endpoint['description']
            parameters[:] = [p for p in parameters if p.get('in') != 'header']
        for name in re.findall(r'{([^}]+)}', path):
            if not any(p.get('in') == 'path' and p.get('name') == name for p in parameters):
                parameters.append({'name': name, 'in': 'path', 'required': True, 'schema': {'type': 'string'}})
        # These route segments all pass through routeID on the server.
        if path.endswith('/legacy-imports/reconcile'):
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type':'string','format':'uuid','not':{'const':'00000000-0000-0000-0000-000000000000'}}
        if '/capture-uploads' in path:
            for parameter in parameters:
                if parameter.get('in') == 'path' and parameter.get('name') in ('t', 'id'):
                    parameter['schema'] = {'type': 'string', 'format': 'uuid', 'not': {'const': '00000000-0000-0000-0000-000000000000'}}
            if endpoint.get('idempotency'):
                parameters[:] = [p for p in parameters if not (p.get('in') == 'header' and p.get('name') == 'Idempotency-Key')]
                parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})
        if any(path.endswith('/collections/{id}/'+suffix) for suffix in ('apply-capabilities','apply-profile','save-edits')):
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type': 'string', 'format': 'uuid', 'not': {'const': '00000000-0000-0000-0000-000000000000'}}
            if method == 'get':
                parameters[:] = [p for p in parameters if p.get('in') != 'query']
                parameters.extend([
                    {'name':'style_version_id','in':'query','required':True,'schema':{'type':'string','format':'uuid','not':{'const':'00000000-0000-0000-0000-000000000000'}}},
                    {'name':'source_revision','in':'query','required':True,'schema':{'type':'string','pattern':'^[1-9][0-9]{0,15}$'}}])
            else:
                parameters[:] = [p for p in parameters if not (p.get('in') == 'header' and p.get('name') == 'Idempotency-Key')]
                parameters.append({'name':'Idempotency-Key','in':'header','required':True,'schema':{'type':'string','minLength':1,'maxLength':128,'pattern':r'^[!-~]+$'}})
        if path.endswith('/collections/{id}/save-edits'):
            parameters[:] = [p for p in parameters if not (p.get('in') == 'header' and p.get('name') == 'Save-Edits-Resolve-Only')]
            parameters.append({'name':'Save-Edits-Resolve-Only','in':'header','required':False,'schema':{'const':'abandon'}})
            operation.setdefault('responses', {})['200'] = {'description':'Abandoned unaccepted request in resolve-only mode','content':{'application/json':{'schema':{'$ref':'./schemas.json#/$defs/CollectionSaveEditsResolution'}}}}
        if path.endswith('/collections/{id}/culling-groups'):
            parameters[:] = [p for p in parameters if p.get('in') != 'query']
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type': 'string', 'format': 'uuid'}
            parameters.extend([
                {'name': 'cursor', 'in': 'query', 'required': False, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 2048}},
                {'name': 'review_id', 'in': 'query', 'required': False, 'schema': {'type': 'string', 'format': 'uuid'}}])
            operation['description'] = endpoint['description']
        if path.endswith('/prepared-reviews/{entry}/{target}/{kind}/{proposal}'):
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'enum': ['defects', 'dust']} if parameter.get('name') == 'kind' else {'type': 'string', 'format': 'uuid'}
        if '/projects/{id}/interchange-jobs/{workflow}/' in path:
            for parameter in parameters:
                if parameter.get('in') == 'path' and parameter.get('name') in ('t', 'id', 'workflow', 'entry', 'override', 'mask'):
                    parameter['required'] = True
                    parameter['schema'] = {'type': 'string', 'format': 'uuid'}
        if any(segment in path for segment in ('/perspective-proposals/{task}', '/panorama-proposals/{task}', '/focus-stack-proposals/{task}')):
            for parameter in parameters:
                if parameter.get('in') != 'path':
                    continue
                name = parameter.get('name')
                if name in ('t', 'id', 'version', 'task'):
                    parameter['schema'] = {'type': 'string', 'format': 'uuid'}
                elif name == 'variant':
                    parameter['schema'] = {'type': 'string', 'enum': ['preview1600', 'preview2560']}
            if method == 'post' and path.endswith('/apply'):
                parameters[:] = [parameter for parameter in parameters if not (parameter.get('in') == 'header' and parameter.get('name') == 'Idempotency-Key')]
                operation['description'] = 'Applies the exact result/root and quality evidence in the decision body. This handler does not consume an Idempotency-Key header; recovery must replay the same immutable decision.'

        multi_entry = any(path.endswith('/projects/{id}/' + suffix) for suffix in ('panorama-capabilities', 'panorama-intents', 'focus-stack-capabilities', 'focus-stack-intents'))
        shown_version = path.endswith('/semantic-targets/{target}/version')
        if multi_entry or shown_version:
            parameters[:] = [p for p in parameters if p.get('in') == 'path']
            for parameter in parameters:
                parameter['schema'] = {'type': 'string', 'format': 'uuid', 'not': {'const': '00000000-0000-0000-0000-000000000000'}}
            if multi_entry:
                # Both actual GET describe and POST submit use tenantRequest(true).
                operation['x-web-mutation-csrf'] = True
                if method == 'post':
                    parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})
            else:
                operation['description'] = 'Materialize the exact shown target version. Empty body and no query parameters; this handler does not consume Idempotency-Key. Current target/session/project/source authority is independently rechecked.'
                operation.pop('requestBody', None)

        if path.endswith('/collections/{id}/items/{item}/version'):
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type': 'string', 'format': 'uuid'}
            parameters[:] = [p for p in parameters if not (p.get('in') == 'header' and p.get('name') in ('Idempotency-Key', 'If-Match'))]
            if method == 'put':
                parameters.append({'name': 'If-Match', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'pattern': '^"[1-9][0-9]{0,18}"$'}, 'description': 'Quoted expected_revision from the body; positive int64. No Idempotency-Key ledger: reconcile GET after an ambiguous response.'})
            operation['description'] = 'Editor-authorized collection version CAS. Read item and collection revision consistently; replace only an accepted direct child of the expected version.'

        if method == 'post' and path in (
                '/api/v1/tenants/{t}/collections/{id}/items',
                '/api/v1/tenants/{t}/collections/{id}/items/{item}/moderation',
                '/api/v1/tenants/{t}/collections/{id}/sections'):
            parameters[:] = [p for p in parameters if p.get('in') != 'query' and not (p.get('in') == 'header' and p.get('name') in ('If-Match', 'Idempotency-Key'))]
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type': 'string', 'format': 'uuid', 'not': {'const': '00000000-0000-0000-0000-000000000000'}}
            if not path.endswith('/moderation'):
                parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})

        if path.endswith('/semantic-intents/{id}/targets/{target}/preview'):
            parameters[:] = [p for p in parameters if p.get('in') != 'query']
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type': 'string', 'format': 'uuid'}
            for name in ('expected_revision', 'target_revision', 'plan_sha', 'candidate_sha'):
                pattern = '^[1-9][0-9]{0,18}$' if name.endswith('revision') else '^[0-9a-f]{64}$'
                parameters.append({'name': name, 'in': 'query', 'required': True,
                    'schema': {'type': 'string', 'pattern': pattern}})
            if method == 'post':
                operation['description'] = 'Admit or recover a proposal preview. Body must be empty. On an ambiguous error, X-Preview-Run-ID preserves the known stable run identity; replay the same query fences.'
                operation['x-empty-body'] = True

        if path.endswith('/projects/{project}/delivery-signing-policies'):
            parameters[:] = [p for p in parameters if p.get('in') != 'query']
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type': 'string', 'format': 'uuid'}
            parameters.append({'name': 'mime', 'in': 'query', 'required': True, 'style': 'form', 'explode': True,
                'schema': {'type': 'array', 'minItems': 1, 'maxItems': 2, 'uniqueItems': True, 'items': {'type': 'string', 'enum': ['image/jpeg', 'image/png']}}})
            operation['description'] = 'Installed signing policies supporting every requested MIME. Repeat mime for distinct JPEG/PNG values; TIFF and unknown query keys are rejected. Empty items is a valid unavailable choice set.'

        if '/delivery-packages' in path:
            for parameter in parameters:
                if parameter.get('in') == 'path' and parameter.get('name') in ('t', 'id', 'run', 'workflow'):
                    parameter['schema'] = {'type': 'string', 'format': 'uuid'}
            if method == 'post' and (path.endswith('/delivery-packages') or path.endswith('/publish') or path.endswith('/download-intent')):
                parameters[:] = [p for p in parameters if not (p.get('in') == 'header' and p.get('name') == 'Idempotency-Key')]
                parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}, 'description': 'Immutable request key: printable ASCII without spaces. Replay the same key and body after an ambiguous response.'})
            queries = []
            if method == 'get' and path.endswith('/delivery-packages'):
                queries = [('project_id', True, {'type': 'string', 'format': 'uuid'}), ('limit', False, {'type': 'integer', 'minimum': 1, 'maximum': 100, 'default': 50}), ('cursor', False, {'type': 'string', 'minLength': 1, 'maxLength': 4096})]
            if method == 'get' and (path.endswith('/items') or path.endswith('/inspection')):
                queries = [('revision', True, {'$ref': 'schemas.json#/$defs/DeliveryRevision'})]
                if path.endswith('/items'):
                    queries += [('limit', False, {'type': 'integer', 'minimum': 1, 'maximum': 200, 'default': 100}), ('after_ordinal', False, {'type': 'integer', 'minimum': 0, 'maximum': 4999}), ('after_preset', False, {'type': 'string', 'format': 'uuid'})]
                    operation['description'] = 'Revision-pinned items. after_ordinal and after_preset must be supplied together; omit both for the first page. Unknown or repeated query parameters are rejected.'
            for name, required, schema in queries:
                parameters[:] = [p for p in parameters if not (p.get('in') == 'query' and p.get('name') == name)]
                parameters.append({'name': name, 'in': 'query', 'required': required, 'schema': schema})

        if path.endswith('/prepared-reviews') or path.endswith('/prepared-reviews/saved'):
            if method == 'get' and not any(p.get('in') == 'query' and p.get('name') == 'after' for p in parameters):
                parameters.append({'name': 'after', 'in': 'query', 'required': False, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 120}})
            if method == 'post' and not any(p.get('name') == 'Idempotency-Key' for p in parameters):
                parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'format': 'uuid'}})
            if method == 'post':
                for parameter in parameters:
                    if parameter.get('in') == 'header' and parameter.get('name') == 'Idempotency-Key':
                        parameter['required'] = True
                        parameter['schema'] = {'type': 'string', 'format': 'uuid'}
            if method == 'get':
                for parameter in parameters:
                    if parameter.get('in') == 'query' and parameter.get('name') == 'after':
                        parameter['schema'] = {'type': 'string', 'minLength': 1, 'maxLength': 120, 'pattern': r'^[0-9a-f-]{36}\.[0-9a-f-]{36}\.(defects|dust)\.[0-9a-f-]{36}$'}
        tenant_admin = (method, path) in {
            ('delete', '/api/v1/tenants/{t}'),
            ('post', '/api/v1/tenants/{t}/transfer-owner'),
            ('post', '/api/v1/tenants/{t}/invites'),
            ('delete', '/api/v1/tenants/{t}/members/{user}'),
            ('get', '/api/v1/tenants/{t}/events'),
        }
        if tenant_admin:
            parameters[:] = [p for p in parameters if p.get('in') == 'path']
            for parameter in parameters:
                parameter['schema'] = {'type': 'string', 'format': 'uuid', 'not': {'const': '00000000-0000-0000-0000-000000000000'}}
            if endpoint.get('revision_required'):
                parameters.append({'name': 'If-Match', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'pattern': '^"[1-9][0-9]{0,18}"$'}, 'description': 'Quoted canonical positive signed int64, range 1..9223372036854775807.'})
            if endpoint.get('idempotency'):
                parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})
            if path.endswith('/events'):
                parameters.extend([{'name': name, 'in': 'query', 'required': False, 'schema': {'$ref': 'schemas.json#/$defs/TenantAdminEventQuery/properties/' + name}} for name in ('after', 'limit')])
                parameters.append({'name': 'Last-Event-ID', 'in': 'header', 'required': False, 'schema': {'type': 'string', 'pattern': '^(0|[1-9][0-9]{0,18})$', 'description': 'Canonical nonnegative signed int64; at most 9223372036854775807.'}})
                operation['description'] = endpoint['description']
        role = endpoint['role']
        operation.setdefault('security', [] if role in ('device_public','public') else [{'deviceBearer' if role == 'device' else 'userBearer': []}])
        operation.setdefault('x-web-mutation-csrf', method != 'get' and role not in ('device', 'device_public'))
        responses = operation.setdefault('responses', {})
        # Keep the current primary response metadata; extra successes are only
        # retained by explicit source-backed replay/status branches below.
        for code in list(responses):
            if code.isdigit() and 200 <= int(code) < 300 and code != str(endpoint['success_status']):
                del responses[code]
        media = endpoint.get('response_media_type', 'application/json')
        success = copy.deepcopy(responses.get(str(endpoint['success_status']), {'description': 'Success'}))
        if endpoint['response_schema']:
            success['content'] = {media: {'schema': {'$ref': 'schemas.json#/$defs/' + endpoint['response_schema']}}}
        if 'response_max_bytes' in endpoint:
            success['x-max-bytes'] = endpoint['response_max_bytes']
        responses[str(endpoint['success_status'])] = success
        if tenant_admin and path.endswith('/events'):
            success['content']['text/event-stream'] = {
                'schema': {'type': 'string'},
                'x-event-schema': {'$ref': 'schemas.json#/$defs/TenantAdminEvent'},
                'description': 'SSE id: cursor, data: complete TenantAdminEvent JSON; heartbeat comments contain no event.'}
        if path.endswith('/collections/{id}/sections/abandon'):
            # Original CreateSection receipt wins under the same key/lock.
            for code, schema in [('200', 'CollectionSectionAbandoned'), ('201', 'CollectionSection')]:
                responses[code] = {'description': 'Authoritative creation resolution', 'content': {'application/json': {'schema': {'$ref': 'schemas.json#/$defs/' + schema}}}}
            parameters[:] = [p for p in parameters if not (p.get('in') == 'header' and p.get('name') == 'Idempotency-Key')]
            parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})

        if path.endswith('/contact-sheets'):
            parameters[:] = [p for p in parameters if not (p.get('in') == 'header' and p.get('name') == 'X-CSRF-Token')]
            parameters.append({'name':'X-CSRF-Token','in':'header','required':True,'schema':{'type':'string'},'description':'Required for cookie-authenticated clients.'})
            parameters[:] = [p for p in parameters if not (p.get('in') == 'header' and p.get('name') == 'Idempotency-Key')]
            parameters.append({'name':'Idempotency-Key','in':'header','required':True,'schema':{'type':'string','minLength':1,'maxLength':128,'pattern':r'^[!-~]+$'}})
            for parameter in parameters:
                if parameter.get('in') == 'path' and parameter.get('name') == 't':
                    parameter['schema'] = {'type':'string','format':'uuid','not':{'const':'00000000-0000-0000-0000-000000000000'}}

        if path.endswith('/contact-sheets/abandon'):
            for code, schema in [('200', 'ContactSheetAbandoned'), ('202', 'ContactSheetAccepted')]:
                responses[code] = {'description': 'Serialized original admission resolution', 'content': {'application/json': {'schema': {'$ref': 'schemas.json#/$defs/' + schema}}}}
            parameters[:] = [p for p in parameters if not (p.get('in') == 'header' and p.get('name') == 'Idempotency-Key')]
            parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})

        if path.endswith('/collections/{id}/bulk-operations/abandon'):
            for code, schema in [('200', 'CollectionBulkAbandoned'), ('202', 'CollectionBulkAcceptedResolution')]:
                responses[code] = {'description': 'Authoritative combined admission resolution', 'content': {'application/json': {'schema': {'$ref': 'schemas.json#/$defs/' + schema}}}}
            parameters[:] = [p for p in parameters if not (p.get('in') == 'header' and p.get('name') == 'Idempotency-Key')]
            parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})

        if path.endswith('/collections/{id}/shares/abandon'):
            for code, schema in [('200', 'ShareCreationAbandoned'), ('201', 'CollectionShareCreated')]:
                responses[code] = {'description': 'Authoritative original share creation resolution', 'content': {'application/json': {'schema': {'$ref': 'schemas.json#/$defs/' + schema}}}}

        # Error status sets are handler-specific. Preserve reviewed entries; do not
        # invent every domain error on every route, including public health.
        if path in (
                '/api/v1/tenants/{t}/shoot-sessions/{id}/analysis-capabilities',
                '/api/v1/tenants/{t}/collections/{id}/shares/{share}/usage',
                '/api/v1/tenants/{t}/collections/{id}/publication'):
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['required'] = True
                    parameter['schema'] = {'type': 'string', 'format': 'uuid',
                        'not': {'const': '00000000-0000-0000-0000-000000000000'}}
            operation['x-idempotency'] = False
            operation['security'] = [{'userBearer': []}]
            operation['x-web-mutation-csrf'] = method != 'get'
            parameters[:] = [p for p in parameters if not (
                p.get('in') == 'header' and p.get('name') in ('Idempotency-Key', 'If-Match'))]
            if path.endswith('/analysis-capabilities'):
                operation['x-read-only-post'] = True
                operation['x-query-parameters-allowed'] = False
                parameters[:] = [p for p in parameters if p.get('in') != 'query']
                success['headers'] = {'Cache-Control': {'schema': {
                    'type': 'string', 'const': 'private, no-store'}}}
            elif path.endswith('/usage'):
                success['headers'] = {'Cache-Control': {'schema': {
                    'type': 'string', 'const': 'no-store'}}}
            else:
                success['headers'] = {'ETag': {
                    'description': 'Quoted new positive signed-int64 collection revision',
                    'schema': {'type': 'string', 'pattern': '^"[1-9][0-9]{0,18}"$'}}}
        if path in ('/api/v1/tenants/{t}/collections/{id}/selection-snapshots', '/api/v1/tenants/{t}/collections/{id}/bulk-operations', '/api/v1/tenants/{t}/bulk-operations/{id}', '/api/v1/tenants/{t}/bulk-operations/{id}/cancel'):
            parameters[:] = [p for p in parameters if p.get('in') not in ('query', 'header')]
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type': 'string', 'format': 'uuid', 'not': {'const': '00000000-0000-0000-0000-000000000000'}}
            if endpoint['idempotency']:
                parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})
            if method == 'get':
                parameters.extend([{'name': 'limit', 'in': 'query', 'required': False, 'schema': {'type': 'integer', 'minimum': 1, 'maximum': 100, 'default': 50}}, {'name': 'cursor', 'in': 'query', 'required': False, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 4096}}])
        if path in ('/api/v1/tenants/{t}/collections/{id}/shares', '/api/v1/tenants/{t}/collections/{id}/shares/abandon', '/api/v1/tenants/{t}/collections/{id}/shares/{share}', '/api/v1/tenants/{t}/collections/{id}/shares/{share}/revoke'):
            parameters[:] = [p for p in parameters if p.get('in') not in ('header', 'query')]
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type': 'string', 'format': 'uuid', 'not': {'const': '00000000-0000-0000-0000-000000000000'}}
            if endpoint['idempotency']:
                parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})
            if method == 'patch':
                parameters.append({'name': 'If-Match', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'pattern': '^"[1-9][0-9]{0,18}"$'}, 'description': 'Quoted signed-int64 policy_version; exact next revision returned in ETag.'})
                success['headers'] = {'ETag': {'schema': {'type': 'string', 'pattern': '^"[1-9][0-9]{0,18}"$'}}}
            if method == 'get':
                parameters.extend([{'name': 'limit', 'in': 'query', 'required': False, 'schema': {'type': 'integer', 'minimum': 1, 'maximum': 100, 'default': 50}}, {'name': 'cursor', 'in': 'query', 'required': False, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 4096}}])
        if method == 'get' and path in ('/api/v1/tenants/{t}/reviews/{review}/multi-replacements', '/api/v1/tenants/{t}/reviews/{review}/perspective-replacements'):
            parameters[:] = [p for p in parameters if p.get('in') != 'query']
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type': 'string', 'format': 'uuid'}
            parameters.extend([
                {'name': 'decision_hash', 'in': 'query', 'required': True, 'schema': {'type': 'string', 'pattern': '^[0-9a-f]{64}$'}},
                {'name': 'after', 'in': 'query', 'required': False, 'schema': {'type': 'string', 'format': 'uuid', 'not': {'const': '00000000-0000-0000-0000-000000000000'}}}])
        if path in ('/api/v1/tenants/{t}/collections', '/api/v1/tenants/{t}/collections/{id}'):
            operation['description'] = endpoint['description']
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type': 'string', 'format': 'uuid'}
            parameters[:] = [p for p in parameters if p.get('in') != 'query' and not (p.get('in') == 'header' and p.get('name') in ('If-Match', 'Idempotency-Key'))]
            if method == 'get' and path.endswith('/collections'):
                for name in ('limit', 'cursor', 'project_id', 'state', 'search'):
                    parameters.append({'name': name, 'in': 'query', 'required': False, 'schema': {'$ref': 'schemas.json#/$defs/CollectionQuery/properties/' + name}})
            if method == 'post':
                parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})
            if method in ('patch', 'delete'):
                parameters.append({'name': 'If-Match', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'pattern': '^"[1-9][0-9]{0,18}"$'}, 'description': 'Quoted canonical positive signed int64, range 1..9223372036854775807; expected collection revision. DELETE body expected_revision must equal this value.'})
        if method == 'get' and path == '/api/v1/tenants/{t}/collections/{id}/items':
            parameters[:] = [p for p in parameters if p.get('in') != 'query']
            for name in ('limit', 'cursor', 'section_id', 'flag', 'tag', 'rating', 'search', 'review', 'visibility'):
                parameters.append({'name': name, 'in': 'query', 'required': False,
                    'schema': {'$ref': 'schemas.json#/$defs/CollectionGalleryQuery/properties/' + name}})
            operation['description'] = endpoint['description']
        if path.startswith('/api/v1/tenants/{t}/download-jobs'):
            parameters[:] = [p for p in parameters if p.get('in') == 'path']
            for parameter in parameters:
                parameter['schema'] = {'type': 'string', 'format': 'uuid', 'not': {'const': '00000000-0000-0000-0000-000000000000'}}
            operation['security'] = [{'userBearer': []}]
            operation['x-web-mutation-csrf'] = method == 'post'
            if endpoint.get('idempotency'):
                parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})
        if path == '/api/v1/tenants/{t}/export-presets':
            parameters[:] = [p for p in parameters if p.get('in') != 'query' and not (p.get('in') == 'header' and p.get('name') == 'Idempotency-Key')]
            for parameter in parameters:
                if parameter.get('in') == 'path':
                    parameter['schema'] = {'type': 'string', 'format': 'uuid'}
            if method == 'get':
                parameters.extend([
                    {'name': 'limit', 'in': 'query', 'required': False, 'schema': {'type': 'integer', 'minimum': 1, 'maximum': 100, 'default': 50}},
                    {'name': 'cursor', 'in': 'query', 'required': False, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 4096}},
                ])
            elif method == 'post':
                parameters.append({'name': 'Idempotency-Key', 'in': 'header', 'required': True, 'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}})
        if path == '/api/v1/tenants/{t}/styles/{id}/analysis-capabilities' and method == 'get':
            parameters[:] = [p for p in parameters if p.get('in') == 'path']
            parameters.append({'name': 'geometry_enabled', 'in': 'query', 'required': True, 'schema': {'type': 'boolean'}})
            operation['security'] = [{'userBearer': []}]
            operation['x-web-mutation-csrf'] = False
        if method == 'post' and path in (
                '/api/v1/tenants/{t}/masks', '/api/v1/tenants/{t}/generations',
                '/api/v1/tenants/{t}/generations/abandon',
                '/api/v1/tenants/{t}/previews', '/api/v1/tenants/{t}/overrides'):
            parameters[:] = [
                {'name': 't', 'in': 'path', 'required': True, 'schema': {
                    'type': 'string', 'format': 'uuid',
                    'not': {'const': '00000000-0000-0000-0000-000000000000'}}},
                {'name': 'Idempotency-Key', 'in': 'header', 'required': True,
                    'schema': {'type': 'string', 'minLength': 1, 'maxLength': 128, 'pattern': r'^[!-~]+$'}}]
            if path.endswith('/generations'):
                parameters.append({'name': 'Generation-Replay-Only', 'in': 'header',
                    'required': False, 'schema': {'type': 'string', 'enum': ['true']},
                    'description': 'Lookup the original immutable receipt without admitting new GPU work; absent receipt returns GENERATION_REPLAY_PENDING.'})
            operation['security'] = [{'userBearer': []}]
            operation['x-idempotency'] = True
            operation['x-web-mutation-csrf'] = True
            success['headers'] = {'Idempotency-Replayed': {'schema': {'type': 'string', 'const': 'true'}}}
            operation['description'] = 'Source-bound interactive editor operation. Current tenant and project Editor authority is required for admission and recovery. Replay uses the same actor, key and canonical body; accepted recovery does not repeat worker qualification.'
            if path.endswith('/generations/abandon'):
                responses['202'] = {'description': 'Original generation already accepted',
                    'content': {'application/json': {'schema': {'$ref': 'schemas.json#/$defs/RunAccepted'}}}}
        if method == 'get' and path == '/api/v1/tenants/{t}/capabilities':
            parameters[:] = [{'name': 't', 'in': 'path', 'required': True,
                'schema': {'type': 'string', 'format': 'uuid',
                    'not': {'const': '00000000-0000-0000-0000-000000000000'}}}]
            operation['security'] = [{'userBearer': []}]
            operation['x-web-mutation-csrf'] = False
            operation['x-idempotency'] = False
        # Some current GET rows omit the optional body schema entirely.
        if endpoint.get('request_schema') and method not in ('get','head'):
            operation['requestBody'] = {'required': True, 'content': {'application/json': {'schema': {'$ref': 'schemas.json#/$defs/' + endpoint['request_schema']}}}}
        else:
            operation.pop('requestBody', None)
        # Keep existing typed per-status replay contracts and physical response headers.
        if path.endswith('/collections/{id}/save-edits'):
            responses['200'] = {'description':'Abandoned unaccepted immutable request; resolve-only mode.', 'content':{'application/json':{'schema':{'$ref':'schemas.json#/$defs/CollectionSaveEditsResolution'}}}}
            responses['202']['content'] = {'application/json':{'schema':{'$ref':'schemas.json#/$defs/CollectionApplyAccepted'}}}
        if path == '/api/v1/tenants/{t}/collections/abandon':
            for code,schema in [('200','CollectionCreationAbandoned'),('201','CollectionCreated')]:
                response=responses.setdefault(code,{'description':'Original creation resolution'})
                response['content']={'application/json':{'schema':{'$ref':'schemas.json#/$defs/'+schema}}}
        if role.startswith('guest:'):
            schemes=result.setdefault('components',{}).setdefault('securitySchemes',{})
            schemes.setdefault('csrfHeader',{'type':'apiKey','in':'header','name':'X-CSRF-Token'})
            schemes['portalSessionCookie']={'type':'apiKey','in':'cookie','name':'__Host-photo_portal_session','description':'Production portal session cookie; development uses photo_dev_portal_session.'}
            operation['security']=[{'portalSessionCookie':[],'csrfHeader':[]}]
            operation['x-web-mutation-csrf']=True
        # routeID accepts UUID syntax; stronger existing nonzero schema remains intact.
        previous=document.get('paths',{}).get(path,{}).get(method,{})
        old_parameters={(v.get('in'),v.get('name')):v for v in previous.get('parameters',[])}
        for parameter in parameters:
            old=old_parameters.get((parameter.get('in'),parameter.get('name')),{}).get('schema',{})
            if parameter.get('in')=='path' and old.get('not') and parameter.get('schema',{}).get('format')=='uuid':
                parameter['schema'].setdefault('not',copy.deepcopy(old['not']))
        if path.endswith('/collections/{id}/apply-capabilities'):
            success.setdefault('headers',{})['Cache-Control']={'schema':{'const':'private, no-store'}}
        # These GET handlers deliberately use tenantRequest(r,true), not merely
        # authenticate: the CSRF contract follows the actual handler, not verb.
        if method == 'get' and path in (
            '/api/v1/tenants/{t}/semantic-targets/{target}/preview',
            '/api/v1/tenants/{t}/runs/{id}/local-portrait-result',
            '/api/v1/tenants/{t}/v2-workflows/{id}/local-portrait-result',
            '/api/v1/tenants/{t}/assets/{id}/region-previews/{run}/descriptor',
            '/api/v1/tenants/{t}/projects/{id}/negative-capabilities',
            '/api/v1/tenants/{t}/semantic-intents/{id}/targets/{target}/preview',
        ):
            operation['x-web-mutation-csrf']=True
        if path == '/api/v1/tenants/{t}/collections/abandon':
            for parameter in parameters:
                if parameter.get('in')=='header' and parameter.get('name')=='Idempotency-Key':
                    parameter['schema']={'type':'string','minLength':1,'maxLength':128,'pattern':r'^[!-~]+$'}
    return result


if __name__ == '__main__':
    directory = ROOT / 'docs/implementation-v2/contracts'
    target = directory / 'openapi.json'
    endpoints = json.loads((directory / 'endpoints.json').read_text())['endpoints']
    document = synchronize(json.loads(target.read_text()), endpoints)
    target.write_text(json.dumps(document, ensure_ascii=False, indent=2) + '\n')
