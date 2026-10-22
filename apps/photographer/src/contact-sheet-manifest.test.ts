import {expect,it} from 'vitest';
import {canonicalJSON} from '@photoeditor/api-client';
import {contactSheetPages} from './contact-sheet-manifest';
import type {RunOutput} from './catalog-types';
const output=(id:string)=>({artifact_id:id,sha256:'a'.repeat(64),role:'contact_page',media_type:'image/png'} as RunOutput);
function manifest(){return {format:'png_pages',labels_embedded:true,locale:'ru',font_lock_id:'b'.repeat(64),total_items:2,pages:['first','second'].map((id,index)=>({artifact:{artifact_id:id,sha256:'a'.repeat(64)},page:index+1,first_index:index,item_count:1,width:320,height:260}))};}
const encode=(value:unknown)=>new TextEncoder().encode(canonicalJSON(value));
it('orders exact outputs by canonical manifest rather than database row order',()=>{
 expect(contactSheetPages(encode(manifest()),[output('second'),output('first')]).map(item=>item.artifact_id)).toEqual(['first','second']);
});
it('rejects missing pages, substituted hashes and noncontiguous pagination',()=>{
 expect(()=>contactSheetPages(encode(manifest()),[output('first')])).toThrow();
 expect(()=>contactSheetPages(encode(manifest()),[output('first'),{...output('second'),sha256:'c'.repeat(64)}])).toThrow();
 const value=manifest();value.pages[1].first_index=0;
 expect(()=>contactSheetPages(encode(value),[output('first'),output('second')])).toThrow();
});
