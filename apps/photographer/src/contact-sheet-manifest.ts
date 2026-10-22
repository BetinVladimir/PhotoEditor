import {canonicalJSON} from '@photoeditor/api-client';
import type {RunOutput} from './catalog-types';
/** Bytes are SHA/size verified by readRegionArtifact before this parser. */
export function contactSheetPages(bytes:Uint8Array,outputs:RunOutput[]):RunOutput[]{
 const fail=():never=>{throw new Error('CONTACT_MANIFEST_INVALID');};
 const raw=new TextDecoder('utf-8',{fatal:true}).decode(bytes),value=JSON.parse(raw);
 if(canonicalJSON(value)!==raw||!value||value.format!=='png_pages'||value.labels_embedded!==true||!['en','ru','bg'].includes(value.locale)||!/^[a-f0-9]{64}$/.test(value.font_lock_id)||!Number.isInteger(value.total_items)||value.total_items<1||value.total_items>10000||!Array.isArray(value.pages)||!value.pages.length||value.pages.length>128)fail();
 const pages=outputs.filter(item=>item.role==='contact_page');
 if(pages.length!==value.pages.length||new Set(pages.map(item=>item.artifact_id)).size!==pages.length)fail();
 let offset=0;const seen=new Set<string>();
 const ordered=value.pages.map((page:any,index:number)=>{
  if(page.page!==index+1||page.first_index!==offset||!Number.isInteger(page.item_count)||page.item_count<1||page.item_count>80||!Number.isInteger(page.width)||page.width<1||page.width>2560||!Number.isInteger(page.height)||page.height<1||page.height>2600||!page.artifact||seen.has(page.artifact.artifact_id))return fail();
  const output=pages.find(item=>item.artifact_id===page.artifact.artifact_id&&item.sha256===page.artifact.sha256&&item.media_type==='image/png');if(!output)return fail();
  offset+=page.item_count;seen.add(output.artifact_id);return {...output};
 });
 if(offset!==value.total_items)fail();return ordered;
}
