import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {canonicalJSON} from '@photoeditor/api-client';
import type {ArtifactRef,V2VersionBinding} from './catalog-types';
export type MaskExpr={op:'ref';mask:ArtifactRef;canvas_hash:string}|{op:'invert';operand:MaskExpr}|{op:'union'|'intersect'|'subtract';left:MaskExpr;right:MaskExpr};
export type EditorLayer={id:string;enabled:boolean;opacity:number;blend:'normal'|'multiply'|'screen';operation:string;operation_version:string;params:ArtifactRef;mask:MaskExpr};
export type EditorLayerStack={schema_version:1;source:V2VersionBinding;layers:EditorLayer[]};
const hash=(value:unknown)=>typeof value==='string'&&/^[0-9a-f]{64}$/.test(value);
const uuid=(value:unknown)=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
function fail():never{throw new Error('EDITOR_LAYERS_INVALID');}
function object(value:unknown,keys:string[]):Record<string,unknown>{if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length!==keys.length||keys.some(key=>!Object.hasOwn(value,key)))return fail();return value as Record<string,unknown>;}
function reference(value:unknown){const ref=object(value,['artifact_id','sha256']);if(!uuid(ref.artifact_id)||!hash(ref.sha256))fail();}
/** Structural validation mirrors Go's existing LayerStack admission; server still authorizes every ref/operation. */
export function readLayerStack(value:unknown,binding:V2VersionBinding):EditorLayerStack{
 const stack=object(value,['schema_version','source','layers']);const source=object(stack.source,['asset_version_id','source','canvas_hash']);reference(source.source);
 if(!uuid(source.asset_version_id)||!hash(source.canvas_hash)||canonicalJSON(source)!==canonicalJSON(binding)||stack.schema_version!==1||!Array.isArray(stack.layers)||stack.layers.length>32)return fail();
 let nodes=0;const ids=new Set<unknown>();let photometric=false;
 function mask(value:unknown,depth=0){if(depth>16||++nodes>256)fail();if(!value||typeof value!=='object')return fail();const op=(value as {op:unknown}).op;
  if(op==='ref'){const ref=object(value,['op','mask','canvas_hash']);reference(ref.mask);if(ref.canvas_hash!==binding.canvas_hash)fail();}
  else if(op==='invert'){const ref=object(value,['op','operand']);mask(ref.operand,depth+1);}
  else if(['union','intersect','subtract'].includes(op as string)){const ref=object(value,['op','left','right']);mask(ref.left,depth+1);mask(ref.right,depth+1);}else fail();
 }
 for(const value of stack.layers){const layer=object(value,['id','enabled','opacity','blend','operation','operation_version','params','mask']);
  if(!uuid(layer.id)||ids.has(layer.id)||typeof layer.enabled!=='boolean'||typeof layer.opacity!=='number'||!Number.isFinite(layer.opacity)||layer.opacity<0||layer.opacity>1||!['normal','multiply','screen'].includes(layer.blend as string)||typeof layer.operation!=='string'||new TextEncoder().encode(layer.operation).length<1||new TextEncoder().encode(layer.operation).length>80||typeof layer.operation_version!=='string'||new TextEncoder().encode(layer.operation_version).length<1||new TextEncoder().encode(layer.operation_version).length>20)fail();ids.add(layer.id);
  if(layer.enabled&&layer.opacity>0){const geometry=['optical.lens','optical.perspective','optical.panorama','optical.focus_stack'].includes(layer.operation as string);if(geometry&&photometric)fail();if(!geometry)photometric=true;}
  reference(layer.params);mask(layer.mask);
 }
 const body=canonicalJSON(stack);if(new TextEncoder().encode(body).byteLength>1048576)fail();return JSON.parse(body) as EditorLayerStack;
}
export function patchEditorLayer(stack:EditorLayerStack,id:string,patch:Partial<Pick<EditorLayer,'enabled'|'opacity'|'blend'>>):EditorLayerStack{
 if(!Object.keys(patch).length||Object.keys(patch).some(key=>!['enabled','opacity','blend'].includes(key))||!stack.layers.some(layer=>layer.id===id))return fail();
 return readLayerStack({...stack,layers:stack.layers.map(layer=>layer.id===id?{...layer,...patch}:layer)},stack.source);
}
export function moveEditorLayer(stack:EditorLayerStack,id:string,index:number):EditorLayerStack{
 if(!Number.isInteger(index)||index<0||index>=stack.layers.length)return fail();const copy=readLayerStack(stack,stack.source);const from=copy.layers.findIndex(layer=>layer.id===id);if(from<0)return fail();const [layer]=copy.layers.splice(from,1);copy.layers.splice(index,0,layer);return readLayerStack(copy,copy.source);
}

export function readLayerDocument(value:unknown,reference:ArtifactRef,binding:V2VersionBinding):EditorLayerStack{
 const stack=readLayerStack(value,binding);
 if(!uuid(reference.artifact_id)||!hash(reference.sha256)||bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(stack))))!==reference.sha256)fail();
 return stack;
}
