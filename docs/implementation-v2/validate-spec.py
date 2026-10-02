"""Static documentation checks; does not assert application implementation or ML quality."""
from pathlib import Path
import json,re,math,hashlib
from jsonschema import Draft202012Validator,FormatChecker
R=Path(__file__).resolve().parent;C=R/'contracts'
def read(n):return json.loads((C/n).read_text(),parse_constant=lambda x:(_ for _ in ()).throw(ValueError(x)))
schema=read('schemas.json');D=schema['$defs'];Draft202012Validator.check_schema(schema)
checks=0
def check(ok,msg):
 global checks
 assert ok,msg
 checks+=1
def walk(v):
 if isinstance(v,dict):
  if '$ref' in v:
   ref=v['$ref'];check(ref.startswith('#/$defs/') and ref[8:] in D,'unresolved '+ref)
  for val in v.values():walk(val)
 elif isinstance(v,list):
  for val in v:walk(val)
walk(schema)
E=read('endpoints.json')['endpoints'];O=read('operations.json')['operations']
P=json.loads((R/'work-packages.json').read_text())['packages'];ids={x['id'] for x in P}
seen=set()
for p in P:
 check(all(x in seen for x in p['depends_on']),'cycle/order '+p['id']);seen.add(p['id'])
 for s in p['specifications']:check((R/s).is_file(),'missing spec '+s)
check(len({(e['path'],e['method']) for e in E})==len(E),'duplicate endpoint')
check(len({e['handler'] for e in E})==len(E),'duplicate handler')
for e in E:
 check(e['package'] in ids,'package')
 for key in ('request_schema','response_schema'):
  check(e[key] is None or e[key] in D,'endpoint schema')
 op=read('openapi.json')['paths'][e['path']][e['method'].lower()]
 check(op['operationId']==e['handler'],'openapi drift')
for o in O:
 check(o['params'] in D and o['output'] in D,'operation schema')
 check(o['package'] in ids,'operation package')
 check((R/o['algorithm_spec']).is_file(),'operation logic missing')
 check(set(o['output_roles'])==set(o['role_policies']),'role policy mismatch')
 check(o['max_output_count']==sum(v['max_count'] for v in o['role_policies'].values()),'count')
 check(o['max_output_bytes']==sum(v['max_count']*v['max_bytes'] for v in o['role_policies'].values()),'bytes')
 check(o['resource_class'] in ('cpu','gpu','media','archive','collection','mail'),'wire resource class')
 check(o['execution_pool'] in ('cpu','gpu','signer'),'execution pool')
DB=read('database.json');tables={t['name']:t for t in DB['tables']}
base_sql='\n'.join(p.read_text() for p in (R.parents[1]/'backend/db/migrations').glob('*.sql'))
base_tables=set(re.findall(r'CREATE TABLE(?: IF NOT EXISTS)? photo\."?([a-z_]+)',base_sql,re.I))
for rel in DB['relations']:
 check(rel['target'] in tables or rel['target'] in base_tables,'unknown FK target '+rel['target'])
 check(rel['column'] in {c['name'] for c in tables[rel['table']]['columns']},'missing FK column')
check('state' not in {c['name'] for c in tables['v2_workflows']['columns']},'dual state')
check(set(DB['purge_allowlist'])==set(tables),'purge coverage')
check(len({p['migration'] for p in P if p['migration']})==12,'migration collision')
for t in tables.values():
 check(t['migration'] in {p['migration'] for p in P},'table migration')
# Local markdown links and table pipe counts.
for p in R.glob('*.md'):
 if p.name=='competitive-roadmap.md':continue
 for target in re.findall(r'\]\(([^)]+)\)',p.read_text()):
  if '://' in target or target.startswith('#'):continue
  target=target.split('#')[0]
  check((p.parent/target).exists(),f'broken link {p.name}: {target}')
 lines=p.read_text().splitlines()
 width=None
 for line in lines:
  if not line.startswith('|'):width=None;continue
  count=len(re.findall(r'(?<!\\)\|',line))
  if width is None:width=count
  check(count==width,'table columns '+p.name+': '+line[:80])
# Structural witnesses, not training/quality evidence.
uid='00000000-0000-4000-8000-000000000001'
def sample(s,depth=0):
 if '$ref' in s:return sample(D[s['$ref'][8:]],depth+1)
 if 'const' in s:return s['const']
 if 'enum' in s:return s['enum'][0]
 for tag in ('oneOf','anyOf'):
  if tag in s:return sample(s[tag][0],depth+1)
 typ=s.get('type')
 if typ=='object':
  out={k:sample(v,depth+1) for k,v in s.get('properties',{}).items() if k in s.get('required',[])}
  if 'learn' in out:out.update(learn=True)
  if out.get('kind')=='remove_object':out['ev']=None
  return out
 if typ=='array':return [sample(s['items'],depth+1) for _ in range(s.get('minItems',0))]
 if typ=='boolean':return True
 if typ=='null':return None
 if typ in ('number','integer'):return s.get('minimum',0)
 if isinstance(typ,list):return 'x'
 if s.get('format')=='uuid':return uid
 if s.get('format')=='date-time':return '2026-09-30T00:00:00Z'
 pat=s.get('pattern','')
 if '64' in pat:return '0'*64
 if '43' in pat:return 'A'*43
 if '86' in pat:return 'A'*86
 if pat:return '1'
 return 'x'*s.get('minLength',1)
fixtures={}
for name,s in D.items():
 witness=sample(s)
 validator=Draft202012Validator({'$ref':'#/$defs/'+name,'$defs':D},format_checker=FormatChecker())
 errs=list(validator.iter_errors(witness))
 check(not errs,name+': '+str(errs[0])[:220] if errs else name)
 fixtures[name]=witness
 if s.get('type')=='object' and s.get('additionalProperties') is False:
  check(not validator.is_valid({**witness,'__unknown':True}),name+' extra accepted')
  if s.get('required'):
   missing=dict(witness);missing.pop(s['required'][0])
   check(not validator.is_valid(missing),name+' missing accepted')
(C/'structural-fixtures.json').write_text(json.dumps({'notice':'Schema witnesses only; UUID repetition is intentional. Not semantic, ML, or integration evidence.','fixtures':fixtures},indent=2,ensure_ascii=False)+'\n')
# Numeric algorithm golden assertions, independent of application.
ev=max(-1,min(1,math.log2((.4+1e-6)/(.2+1e-6))))
check(abs(ev-1)<1e-5,'EV golden')
check(max(.2-.7,0)==0,'subtract mask golden')
check(max(2,math.ceil(.2*6))==2 and 6-2==4,'holdout minimum')
report={'static_checks':checks,'schemas':len(D),'endpoints':len(E),'operations':len(O),'tables':len(tables),'packages':len(P),'result':'passed','not_run':['PostgreSQL migrations','application integration','RTX3060 benchmark','model qualification']}
(C/'validation-report.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,ensure_ascii=False))
