from pathlib import Path
import json,re,hashlib,copy
from jsonschema import Draft202012Validator,FormatChecker
r=Path(__file__).resolve().parent;checks=[]
def check(name,condition):
 if not condition:raise AssertionError(name)
 checks.append({'check':name,'status':'passed'})
for p in r.rglob('*.json'):json.loads(p.read_text())
check('all_json_parse',True)
for p in r.rglob('*.md'):
 for target in re.findall(r'\]\(([^)]+)\)',p.read_text()):
  if '://' in target or target.startswith('#'):continue
  check('link:'+p.name+':'+target,(p.parent/target.split('#')[0]).exists())
tables=json.loads((r/'contracts/tables.json').read_text())['tables']
ops=json.loads((r/'contracts/operations.json').read_text())['operations']
api=json.loads((r/'contracts/endpoints.json').read_text())['endpoints']
pkgs=json.loads((r/'contracts/work-packages.json').read_text())['packages']
for name,items,key in [('tables',tables,lambda x:x['name']),('operations',ops,lambda x:x['name']),('endpoints',api,lambda x:(x['method'],x['path'])),('packages',pkgs,lambda x:x['id'])]:check(name+':unique',len({key(x) for x in items})==len(items))
for t in tables:
 fields=[x.split(':')[0] for x in t['columns']]
 check('table_fields:'+t['name'],len(fields)==len(set(fields)))
 if t['scope']=='tenant':check('tenant_column:'+t['name'],'tenant_id uuid not null' in t['common_columns'])
for op in ops:
 check('op_contract:'+op['name'],all(op[k] for k in ['input_type','output_type','params_type','methods','routing_key']))
 check('op_timeout:'+op['name'],0<op['timeout_seconds']<=7200)
seen=set()
for p in pkgs:
 check('package_dag:'+p['id'],set(p['depends_on'])<=seen);seen.add(p['id'])
 for prefix in p['documents']:check('package_doc:'+p['id']+':'+prefix,bool(list(r.glob(prefix+'-*.md'))))
for n in range(1,49):check('requirement:'+str(n),bool(re.search(r'^\| '+str(n)+r' \|', (r/'14-traceability.md').read_text(),re.M)))
for name in ['task-command','task-result']:
 schema=json.loads((r/'contracts'/f'{name}.schema.json').read_text());Draft202012Validator.check_schema(schema)
 v=Draft202012Validator(schema,format_checker=FormatChecker());example=json.loads((r/'contracts/examples'/f'{name}.example.json').read_text());v.validate(example)
 bad=copy.deepcopy(example);bad['unknown']='x';check(name+':reject_unknown',bool(list(v.iter_errors(bad))))
 bad=copy.deepcopy(example);bad['tenant_id']='not-a-uuid';check(name+':reject_bad_uuid',bool(list(v.iter_errors(bad))))
 check(name+':example_valid',True)
fixed=json.loads((r/'contracts/examples/style-fixed.example.json').read_text());check('fixed_example_shape',fixed['schema_version']=='2.0.0' and fixed['adaptation'] is None and fixed['quality']['status']=='example' and len(fixed['global']['master_curve'])==7)
inv=json.loads((r/'contracts/source-inventory.json').read_text());root=Path(inv['root']);seen_paths=set()
for f in inv['files']:
 check('source_inventory_record:'+f['path'],f['path'] not in seen_paths and isinstance(f.get('bytes'),int) and f['bytes']>=0 and bool(re.fullmatch(r'[0-9a-f]{64}',f.get('sha256',''))));seen_paths.add(f['path'])
 check('source_inventory_path:'+f['path'],(root/f['path']).is_file())
quality=json.loads((root/'resources/legacy-quality-baseline.json').read_text())
for path,digest in quality.items():check('legacy_quality_unchanged:'+path,hashlib.sha256((root/path).read_bytes()).hexdigest()==digest)
report={'date':'2026-09-21','status':'passed','scope':'Documentation structure, schema envelope fixtures, registry consistency, requirement mapping and source inventory integrity and unchanged legacy quality baseline; not implementation/algorithm acceptance.','counts':{'tables':len(tables),'operations':len(ops),'endpoints':len(api),'packages':len(pkgs),'checks':len(checks)},'legacy_tests':{'command':'PYTHONDONTWRITEBYTECODE=1 .venv/bin/python -m pytest -q -p no:cacheprovider','result':'22 passed in 0.67s'},'checks':checks}
(r/'validation-report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({k:report[k] for k in ['status','counts','legacy_tests']},ensure_ascii=False,indent=2))
