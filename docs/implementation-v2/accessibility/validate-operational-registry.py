"""Read-only documentation integrity check. Never tests application accessibility."""
from pathlib import Path
from collections import Counter
import hashlib,json
HERE=Path(__file__).resolve().parent;ROOT=HERE.parents[2]
EXPECTED_A=set('1.1.1 1.2.1 1.2.2 1.2.3 1.3.1 1.3.2 1.3.3 1.4.1 1.4.2 2.1.1 2.1.2 2.1.4 2.2.1 2.2.2 2.3.1 2.4.1 2.4.2 2.4.3 2.4.4 2.5.1 2.5.2 2.5.3 2.5.4 3.1.1 3.2.1 3.2.2 3.2.6 3.3.1 3.3.2 3.3.7 4.1.2'.split())
EXPECTED_AA=set('1.2.4 1.2.5 1.3.4 1.3.5 1.4.3 1.4.4 1.4.5 1.4.10 1.4.11 1.4.12 1.4.13 2.4.5 2.4.6 2.4.7 2.4.11 2.5.7 2.5.8 3.1.2 3.2.3 3.2.4 3.3.3 3.3.4 3.3.8 4.1.3'.split())
def require(value,code):
 if not value:raise ValueError(code)
def load(name):return json.loads((HERE/name).read_text())
def validate():
 cat=load('wcag22-aa-criteria.json');rows=cat['criteria'];ids=[x['id']for x in rows]
 require(len(ids)==len(set(ids))==55,'EXACT55_UNIQUE')
 for level,want in [('A',EXPECTED_A),('AA',EXPECTED_AA)]:require({x['id']for x in rows if x['level']==level}==want,'EXACT_LEVEL_'+level)
 for c in rows:require(c['method']and c['evidence_expected']and c['normative_url'].startswith('https://www.w3.org/TR/WCAG22/#'),'CRITERION_METHOD_SOURCE')
 reg=load('application-platform-registry.json');profiles=reg['profiles'];require(len({x['id']for x in profiles})==len(profiles)==16,'EXACT16_PROFILES')
 totals=Counter()
 for profile in profiles:
  require(profile['status']=='NOT_TESTED','NO_PLATFORM_CONFORMANCE_CLAIM')
  entries=profile['criteria'];require(len(entries)==55 and {x['id']for x in entries}==set(ids),'PROFILE55:'+profile['id'])
  for item in entries:
   require(item['status']=='NOT_TESTED'and item['applicability']=='UNASSESSED'and item['evidence']==[],'NO_INVENTED_INITIAL_RESULT')
   totals[item['status']]+=1
 require(dict(totals)=={'NOT_TESTED':880},'EXACT880_INITIAL_RESULTS')
 routes=load('route-inventory.json');require(len(routes['photographer'])==42 and len(routes['portal'])==3,'EXACT45_ROUTES')
 selected={str(p.relative_to(ROOT)) for p in (ROOT/'apps/photographer/app/[locale]').rglob('*.tsx') if p.name!='_layout.tsx'}
 require({row['source'] for row in routes['photographer']}==selected,'EXACT_CURRENT_LOCALIZED_SCREEN_COVERAGE')
 for row in routes['photographer']:
  for key in ['source','screen_source','legacy_native_source']:require((ROOT/row[key]).is_file(),'CURRENT_SCREEN_SOURCE:'+key)
 for row in routes['portal']:require((ROOT/row['source']).is_file(),'CURRENT_PORTAL_SOURCE')
 font=load('font-contrast-state-registry.json')
 require(font['logical_displayed_screens']==len(font['screens'])==44,'EXACT44_DISPLAYED_FONT_CONTRAST_SCREENS')
 require({(row['application'],row['route']) for row in font['screens']}=={(app,row['route']) for app in ['photographer','portal'] for row in routes[app] if row.get('rendered_surface') is not False},'FONT_CONTRAST_SCREEN_COVERAGE')
 for row in font['screens']:
  require(row['status'] in ('NOT_TESTED','PARTIALLY_TESTED'),'NO_WHOLE_SCREEN_PASS_WITHOUT_COVERAGE')
  require(bool(row['measurements']) == (row['status']=='PARTIALLY_TESTED'),'MEASUREMENT_STATUS_MATCH')
  if row['status']=='PARTIALLY_TESTED':require(bool(row.get('remaining_coverage')),'PARTIAL_REQUIRES_OPEN_COVERAGE')
  for measurement in row['measurements']:
   require(set(font['measurement_fields']).issubset(measurement),'RENDERED_MEASUREMENT_FIELDS')
   ref=Path(measurement['evidence']);require(not ref.is_absolute() and '..' not in ref.parts,'CLOSED_MEASUREMENT_REF')
   evidence=ROOT/ref
   require(evidence.is_file() and hashlib.sha256(evidence.read_bytes()).hexdigest()==measurement['evidence_sha256'],'MEASUREMENT_RECEIPT_HASH')
   require(measurement['platform'] in row['platforms'] and measurement['locale'] in row['locales'],'MEASUREMENT_PLATFORM_LOCALE')
   receipt=json.loads(evidence.read_text());samples=receipt.get('samples',[{'locale':'en','state':'initial','rows':receipt.get('rows',[])}])
   matches=[item for sample in samples if sample['locale']==measurement['locale'] and sample['state']==measurement['state'] for item in sample['rows'] if item.get('text')==measurement['element'] and item.get('contrast_ratio')==measurement['contrast_ratio'] and float(item.get('fontSize','0px')[:-2])==measurement['font_size'] and item.get('color')==measurement['foreground'] and next((color for color in item.get('backgrounds',[]) if color.startswith('rgb(')),None)==measurement['background']]
   require(len(matches)==1,'MEASUREMENT_EXACT_RECEIPT_ROW')
   require(measurement['route']==row['route'].replace('[locale]',measurement['locale']),'MEASUREMENT_ROUTE_MATCH')
   source_pins=measurement.get('source_sha256',{});require(row['source'] in source_pins,'MEASUREMENT_SCREEN_SOURCE_PIN')
   for source_path,want in source_pins.items():
    ref=Path(source_path);require(not ref.is_absolute() and '..' not in ref.parts,'CLOSED_MEASUREMENT_SOURCE')
    source=ROOT/ref;require(source.is_file() and hashlib.sha256(source.read_bytes()).hexdigest()==want,'MEASUREMENT_SOURCE_DRIFT')
   require(type(measurement['font_size']) in (int,float) and measurement['font_size']>0 and type(measurement['contrast_ratio']) in (int,float) and 1<=measurement['contrast_ratio']<=21,'MEASUREMENT_NUMERIC_RANGE')
 native=load('native-screen-state-inventory.json');require(len(native['local_editor']['screens'])==15,'EXACT15_QT_STATES')
 source=load('supporting-evidence-registry.json');drift=[]
 for record in source['records']:
  p=HERE/record['path'];require(p.is_file()and hashlib.sha256(p.read_bytes()).hexdigest()==record['sha256'],'SUPPORTING_RECEIPT_HASH')
  require(record['criterion_result']=='NOT_TESTED','SUPPORTING_CHECK_NOT_FULL_PASS')
 for path,want in source['current_source_snapshot'].items():
  p=Path(path);require(not p.is_absolute()and '..'not in p.parts,'CLOSED_SOURCE_REF')
  f=ROOT/p
  if not f.is_file()or hashlib.sha256(f.read_bytes()).hexdigest()!=want:drift.append(path)
 return {'documentation_integrity':'PASS','A':31,'AA':24,'profiles':16,'NOT_TESTED':880,'routes':45,'Qt_states':15,'source_snapshot_drift':drift,'accessibility_conformance':'NOT_ASSESSED','runtime_executed':False}
if __name__=='__main__':print(json.dumps(validate(),indent=2))
