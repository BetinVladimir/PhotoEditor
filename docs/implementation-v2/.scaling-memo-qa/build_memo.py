from pathlib import Path
from zipfile import ZipFile, ZIP_DEFLATED
from lxml import etree as E
from copy import deepcopy
from hashlib import sha256
import json
R=Path('/Users/freelancer/Documents/PhotoEditor/docs/implementation-v2')
Q=R/'.scaling-memo-qa'
SRC=Path('/Users/freelancer/.codex/plugins/cache/openai-curated-remote/openai-templates/0.1.1/skills/artifact-template-legal-memorandum/assets/reference.docx')
OUT=R/'PhotoEditor_scaling_memorandum.docx'
W='http://schemas.openxmlformats.org/wordprocessingml/2006/main'
ns={'w':W}
tag=lambda n:'{'+W+'}'+n
with ZipFile(SRC) as z: parts={n:z.read(n) for n in z.namelist()}
doc=E.fromstring(parts['word/document.xml']);body=doc.find(tag('body'));old=list(body)
def clone(index,text):
 p=deepcopy(old[index])
 for k in list(p.attrib): del p.attrib[k]
 for ch in list(p):
  if ch.tag!=tag('pPr'):p.remove(ch)
 r=E.SubElement(p,tag('r'))
 source=old[index].find(tag('r'))
 if source is not None and source.find(tag('rPr')) is not None:r.append(deepcopy(source.find(tag('rPr'))))
 t=E.SubElement(r,tag('t'));t.set('{http://www.w3.org/XML/1998/namespace}space','preserve');t.text=text
 return p
def add(p):body.append(p)
for ch in list(body):body.remove(ch)
add(deepcopy(old[0]))
title=clone(1,'Масштабирование PhotoEditor')
pr=title.find(tag('pPr'))
st=E.Element(tag('pStyle'));st.set(tag('val'),'Title');pr.insert(0,st)
# Match source title line rhythm despite semantic Title style.
spacing=E.SubElement(pr,tag('spacing'));spacing.set(tag('after'),'160');spacing.set(tag('line'),'278');spacing.set(tag('lineRule'),'auto')
add(title);add(deepcopy(old[2]))
for idx,label,value in [(3,'Кому','Владельцу и команде PhotoEditor'),(5,'Автор','Codex'),(6,'Дата','30 сентября 2026'),(7,'Тема','Архитектура роста и эксплуатация')]:
 p=deepcopy(old[idx])
 texts=p.xpath('.//w:t',namespaces=ns)
 texts[0].text=label
 texts[1].text=': '
 texts[-1].text=value
 tabs=E.SubElement(p.find(tag('pPr')),tag('tabs'))
 tab=E.SubElement(tabs,tag('tab'));tab.set(tag('val'),'left');tab.set(tag('pos'),'1080')
 add(p)
add(deepcopy(old[8]));add(deepcopy(old[9]))
for line in (Q/'content.txt').read_text().splitlines():
 if not line:continue
 if line.startswith('# '):
  p=clone(10,line[2:])
  pr=p.find(tag('pPr'))
  E.SubElement(pr,tag('keepNext')).set(tag('val'),'1')
  E.SubElement(pr,tag('keepLines')).set(tag('val'),'1')
 else:
  p=clone(18,line)
  E.SubElement(p.find(tag('pPr')),tag('widowControl')).set(tag('val'),'1')
 add(p)
add(deepcopy(old[-1]))
parts['word/document.xml']=E.tostring(doc,xml_declaration=True,encoding='UTF-8',standalone=True)
header=E.fromstring(parts['word/header1.xml'])
header.xpath('//w:t',namespaces=ns)[0].text='PHOTOEDITOR  •  ТЕХНИЧЕСКИЙ МЕМОРАНДУМ'
parts['word/header1.xml']=E.tostring(header,xml_declaration=True,encoding='UTF-8',standalone=True)
settings=E.fromstring(parts['word/settings.xml'])
for e in settings.findall(tag('updateFields')):settings.remove(e)
E.SubElement(settings,tag('updateFields')).set(tag('val'),'true')
parts['word/settings.xml']=E.tostring(settings,xml_declaration=True,encoding='UTF-8',standalone=True)
with ZipFile(OUT,'w',ZIP_DEFLATED) as z:
 for n,b in parts.items():z.writestr(n,b)
inventory=json.loads((Q/'package-inventory.json').read_text())
for n,rec in inventory.items():
 if rec['policy']=='preserve-only':assert sha256(parts[n]).hexdigest()==rec['sha256'],n
assert [(e.tag,dict(e.attrib),e.text) for e in body[-1].iter()]==[(e.tag,dict(e.attrib),e.text) for e in old[-1].iter()]
assert '[Recipient' not in parts['word/document.xml'].decode()
(Q/'fidelity-report.json').write_text(json.dumps({'reference_sha256':sha256(SRC.read_bytes()).hexdigest(),'preserve_only_parts':'all byte-identical','changed_parts':['word/document.xml','word/header1.xml','word/settings.xml'],'section_geometry':'unchanged','word_fields':'update on open','output':str(OUT)},indent=2))
print(OUT)
