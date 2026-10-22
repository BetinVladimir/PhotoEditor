import {canonicalJSON} from '@photoeditor/api-client';
import type {RunOutput} from './catalog-types';
import {readRegionArtifact} from './region-preview-download';
/** Mask documents use the same pinned, bounded byte reader as other artifacts. */
export async function readEditorMaskArtifact(reference:RunOutput,role:string,signal:AbortSignal,transport:typeof fetch=fetch):Promise<unknown>{
 if(!['prompt_mask_ref','rasterized_mask'].includes(role)||reference.role!==role||reference.media_type!=='application/json')throw new Error('EDITOR_MASK_ARTIFACT_INVALID');
 const bytes=await readRegionArtifact({...reference,role:'document'},'manifest',signal,transport);signal.throwIfAborted();
 const text=new TextDecoder('utf-8',{fatal:true}).decode(bytes),value:unknown=JSON.parse(text);
 if(canonicalJSON(value)!==text)throw new Error('EDITOR_MASK_ARTIFACT_INVALID');
 return value;
}
