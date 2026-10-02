"""Rebuild and check documentation only. Run with repo Python containing jsonschema."""
from pathlib import Path
import hashlib,json,os,subprocess,sys
R=Path(__file__).resolve().parent;repo=R.parents[1]
env={**os.environ,'PYTHONDONTWRITEBYTECODE':'1'}
(R/'contracts/check-report.json').write_text(json.dumps({'result':'incomplete','scope':'Documentation checks; incomplete until command succeeds'})+'\n')
lock=json.loads((R/'contracts/source-baseline-lock.json').read_text())
for entry in lock['files']:
    actual=hashlib.sha256((repo/entry['path']).read_bytes()).hexdigest()
    if actual!=entry['sha256']:raise SystemExit('BASELINE_DRIFT: re-review '+entry['path'])
for script in ('build-database.py','build-spec.py','build-indexes.py','validate-spec.py','semantic-spec.py'):
    subprocess.run([sys.executable,str(R/script)],check=True,cwd=repo,env=env)
# Deterministic regeneration check: compare all generator-owned output bytes.
paths=['08-operation-params.md']+['contracts/'+n for n in ('database.json','schema-reference.sql','schemas.json','operations.json','endpoints.json','openapi.json','function-index.json')]
before={p:hashlib.sha256((R/p).read_bytes()).hexdigest() for p in paths}
for script in ('build-database.py','build-spec.py','build-indexes.py'):
    subprocess.run([sys.executable,str(R/script)],check=True,cwd=repo,env=env,stdout=subprocess.DEVNULL)
after={p:hashlib.sha256((R/p).read_bytes()).hexdigest() for p in paths}
if before!=after:raise SystemExit('NONDETERMINISTIC_DOCUMENT_GENERATOR')
report={'result':'passed','source_baseline_files':len(lock['files']),'deterministic_generated_files':len(paths),'static':json.loads((R/'contracts/validation-report.json').read_text()),'semantic':json.loads((R/'contracts/semantic-validation-report.json').read_text()),'scope':'Documentation consistency and reference scenarios only; no production changes, migration execution or ML qualification.'}
(R/'contracts/check-report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
print('Documentation checks passed; regeneration deterministic.')
