import {expect,it} from 'vitest';
import {contactSheetOutputURL} from './contact-sheet-output-url';
import type {RunOutput} from './catalog-types';
const item:RunOutput={artifact_id:'page',role:'contact_page',sha256:'a'.repeat(64),bytes:100,media_type:'image/png',url:'https://objects.example/page?versionId=v1',expires_at:new Date(10000).toISOString()};
it('accepts exact versioned HTTPS output and local development links',()=>{
 expect(contactSheetOutputURL(item,1)).toBe(item.url);
 expect(contactSheetOutputURL({...item,url:'http://127.0.0.1:9000/page?versionId=v1'},1)).toContain('versionId=v1');
});
it.each(['https://objects.example/page','https://objects.example/page?versionId=null','https://objects.example/page?versionId=v1&versionId=v2','http://objects.example/page?versionId=v1','https://user:secret@objects.example/page?versionId=v1','https://objects.example/page?versionId=v1#fragment'])('rejects unbound or unsafe output URL %s',url=>{
 expect(()=>contactSheetOutputURL({...item,url},1)).toThrow('CONTACT_OUTPUT_INVALID');
});
it('rechecks expiry at open time and rejects wrong output kinds',()=>{
 expect(()=>contactSheetOutputURL(item,10000)).toThrow();
 expect(()=>contactSheetOutputURL({...item,role:'working_image'},1)).toThrow();
});
