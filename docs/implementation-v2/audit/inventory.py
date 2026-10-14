"""Read-only source inventory; never treats static matches as acceptance."""
import hashlib,json,re
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
DEST=Path(__file__).resolve().parent
EXCLUDED={'node_modules','__pycache__','.next','.expo','.git','dist','build'}
def files(base,suffixes):
 return sorted(p for p in (ROOT/base).rglob('*') if p.is_file() and p.suffix in suffixes and not EXCLUDED.intersection(p.parts))
def relative(p):return str(p.relative_to(ROOT))
def item(p,**values):return dict(file=relative(p),sha256=hashlib.sha256(p.read_bytes()).hexdigest(),**values)
def module(path):
 for name in ('capture','sessions','styles','editor','semantic','reviews','exports','imports','collections','projects','settings'):
  if name in path.split('/'):return name
 return 'shell_auth' if 'photographer' in path else 'portal'
screens=[]
for base in ('apps/photographer/app','apps/portal/src/app'):
 for p in files(base,{'.tsx'}):
  if p.name not in ('layout.tsx','_layout.tsx') and (base.endswith('photographer/app') or p.name=='page.tsx'):
   screens.append(item(p,module=module(relative(p)),route_file=str(p.relative_to(ROOT/base)),verification='not_verified_current_runtime'))
components=[];states=[]
for base in ('apps/photographer','apps/portal'):
 for p in files(base,{'.ts','.tsx'}):
  if '.test.' in p.name or '.spec.' in p.name or p.name.endswith('.d.ts'):continue
  raw=p.read_text()
  names=re.findall(r'export\s+(?:default\s+)?(?:function|const|class)\s+([A-Z][A-Za-z0-9_]*)',raw)
  if names:components.append(item(p,exports=names))
  for lineno,line in enumerate(raw.splitlines(),1):
   if re.search(r'\b(useState|useReducer|status|state|busy|loading|error|retry|offline|conflict)\b',line):
    states.append(dict(file=relative(p),line=lineno,source=line[:1500],status='static_candidate_requires_behavior_trace'))
backend=[]
for p in files('backend/internal/httpapi',{'.go'}):
 if p.name.endswith('_test.go'):continue
 for lineno,line in enumerate(p.read_text().splitlines(),1):
  for match in re.finditer(r'\.((?:Get|Post|Put|Patch|Delete|Route|Mount|Handle|HandleFunc))\("([^\"]+)"',line):
   backend.append(dict(file=relative(p),line=lineno,method=match.group(1).upper(),path=match.group(2),verification='static_declaration_only'))
functions=json.loads((ROOT/'docs/implementation-v2/contracts/function-index.json').read_text())['functions']
planned=[dict(package=f['package'],name=f['name'],http=f.get('http'),status='requires_current_end_to_end_trace') for f in functions]
ops=json.loads((ROOT/'docs/implementation-v2/contracts/operations.json').read_text())['operations']
packages=json.loads((ROOT/'docs/implementation-v2/work-packages.json').read_text())['packages']
report=dict(schema_version=1,scope='All currently discovered application screens/components/state candidates/API route declarations plus V20-V32 catalogue',
 modules=[dict(id=p['id'],scope=p['scope'],acceptance=p['acceptance'],status='not_accepted_by_inventory') for p in packages],
 screens=screens,components=components,state_candidates=states,api_declarations=backend,planned_entrypoints=planned,
 operations=[dict(id=o['id'],package=o['package'],version=o['version'],status='registry_contract_only') for o in ops],
 limitations=['Static inventory is not implementation proof','Nested router prefixes need manual resolution','Runtime-generated components and states need manual trace','Paper-only named contract not found; normative module criteria are implementation-v2 per user clarification','Historical evidence not counted as current acceptance'])
(DEST/'module-inventory.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
lines=['# Инвентарь функциональных модулей','', 'Источник: текущие файлы; SHA каждого экрана/компонента в module-inventory.json. Статические совпадения не подтверждают реализацию или приёмку.','',f'Найдено экранов: {len(screens)}; файлов с экспортируемыми компонентами: {len(components)}; объявлений маршрутов API: {len(backend)}; кандидатов состояний: {len(states)}.','', '## Все обнаруженные экраны','']
for s in screens:lines.append(f"- `{s['module']}` — `{s['file']}` — требует текущей сквозной проверки")
lines+=['','## Охват модулей V20–V32','']
for p in packages:lines += [f"### {p['id']}",'',p['scope'],'',f"Приёмка: {p['acceptance']}",'','Статус: текущий полный цикл не доказан.','']
lines+=['## Ограничения','',*['- '+v for v in report['limitations']]]
(DEST/'module-inventory.md').write_text('\n'.join(lines)+'\n')
print(json.dumps({key:len(report[key]) for key in ('modules','screens','components','state_candidates','api_declarations','planned_entrypoints','operations')}))
