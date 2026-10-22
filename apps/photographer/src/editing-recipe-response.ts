import type {V2EditingRecipe} from './catalog-types';
import {scopeUUID} from './workspace-access';
import {isRenderSourceBinding} from './render-batch';
const record=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const exact=(v:unknown,keys:string[]):v is Record<string,unknown>=>record(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));
const ref=(v:unknown)=>exact(v,['artifact_id','sha256'])&&scopeUUID(v.artifact_id)&&typeof v.sha256==='string'&&/^[0-9a-f]{64}$/.test(v.sha256);
/** Authored EditingRecipeV3 wire shape, matching ValidateEditingRecipeV3.
 * Executed-base receipts belong to internal frozen render recipes, not editable drafts. */
export function readEditingRecipe(value:unknown):V2EditingRecipe{
 const invalid=():never=>{throw new Error('EDITING_RECIPE_INVALID')};
 const keys=['schema_version','source','base_style','session_corrections','learning_version_id','overrides','layer_stack','renderer_version','parameter_delta'];
 if(!record(value)||!(exact(value,keys)||exact(value,[...keys,'protected_mask']))||value.schema_version!==3||value.renderer_version!=='v2.2'||!isRenderSourceBinding(value.source)||!ref(value.base_style)||value.session_corrections!==null&&!ref(value.session_corrections)||value.layer_stack!==null&&!ref(value.layer_stack)||value.learning_version_id!==null&&!scopeUUID(value.learning_version_id)||!Array.isArray(value.overrides)||value.overrides.length>128||!value.overrides.every(ref)||Object.hasOwn(value,'protected_mask')&&!ref(value.protected_mask))return invalid();
 const delta=value.parameter_delta,bounds={ev:1,log_r:0.223144,log_g:0.223144,log_b:0.223144,saturation_delta:0.15,retouch_delta:0.15};
 if(!exact(delta,Object.keys(bounds)))return invalid();
 for(const [key,max]of Object.entries(bounds))if(typeof delta[key]!=='number'||!Number.isFinite(delta[key])||Math.abs(delta[key] as number)>max)return invalid();
 if(new TextEncoder().encode(JSON.stringify(value)).length>1048576)return invalid();
 return structuredClone(value) as V2EditingRecipe;
}
