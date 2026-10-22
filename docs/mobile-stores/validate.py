"""Offline metadata bounds only; does not validate readiness or legal declarations."""
import json
from pathlib import Path
D=Path(__file__).resolve().parent
LIMITS={'apple':{'name':30,'subtitle':30,'description':4000,'keywords':100},'google':{'name':30,'short_description':80,'full_description':4000}}
for locale in ('en','ru','bg'):
 value=json.loads((D/f'listing-{locale}.json').read_text())
 assert value['locale']==locale and value['status']=='DRAFT_INTERNAL_TESTING_ONLY'
 for store,fields in LIMITS.items():
  assert set(value[store])==set(fields)
  for field,limit in fields.items():
   text=value[store][field]
   assert isinstance(text,str) and text.strip()==text and text
   assert len(text)<=limit and len(text.encode('utf-16-le'))//2<=limit,(locale,store,field)
print('PASS: 3 locales; 21 fields within bounds; no submission/readiness claim')
