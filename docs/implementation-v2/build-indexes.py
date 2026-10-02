"""Build documentation indexes; baseline source lock is deliberately not refreshed."""
from pathlib import Path
import json
R=Path(__file__).resolve().parent;C=R/'contracts'
ops=json.loads((C/'operations.json').read_text())['operations']
defs=json.loads((C/'schemas.json').read_text())['$defs']
endpoints=json.loads((C/'endpoints.json').read_text())['endpoints']
header='# Параметры операций v2.2\n\nНормативные типы и границы — [schemas.json](contracts/schemas.json), роли и лимиты — [operations.json](contracts/operations.json). Artifact closure и lifecycle — [15](15-execution-contracts.md); формулы image processing — [16](16-imaging-algorithms.md); wire protocol — [19](19-runtime-protocol.md). Все поля required; nullable явно задаётся схемой. Таблица генерируется build-indexes.py.\n\n| Операция | Params schema | Поля кроме schema_version | Output | Пакет |\n|---|---|---|---|---|\n'
for o in ops:header+='| '+o['id']+' | '+o['params']+' | '+', '.join(k for k in defs[o['params']]['properties'] if k!='schema_version')+' | '+o['output']+' | '+o['package']+' |\n'
header+='\nDTO добавлять в existing contract generator. Go strict decoding, TS generated types, Pydantic forbid extras; semantic validation15–19 обязательна на сервере и перед исполнением worker. Cross-language tests: unknown fields, null, finite numeric bounds, duplicate IDs, order, byte caps и revision overflow.\n'
(R/'08-operation-params.md').write_text(header)
functions=[]
for e in endpoints:functions.append({'name':e['handler'],'layer':'http_service','package':e['package'],'input':e['request_schema'],'output':e['response_schema'],'behavior':['13-api-behavior.md','15-execution-contracts.md','17-integration-security.md'],'http':e['method']+' '+e['path']})
for o in ops:functions.append({'name':o['id']+'.execute','layer':'worker','package':o['package'],'input':o['params'],'output':o['output'],'behavior':[o['algorithm_spec'],'19-runtime-protocol.md'],'pool':o['execution_pool']})
(C/'function-index.json').write_text(json.dumps({'notice':'Entry point index; helper formulas and SQL functions in referenced normative documents. Not proof of implementation.','functions':functions},ensure_ascii=False,indent=2)+'\n')
print(len(functions),'entry points indexed')
