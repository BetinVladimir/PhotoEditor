import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {expect,it} from 'vitest';
import {readLayerDocument,readLayerStack,moveEditorLayer,patchEditorLayer,type EditorLayerStack,type MaskExpr} from './editor-layers';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
function fixture():EditorLayerStack{const source={asset_version_id:id(1),source:{artifact_id:id(2),sha256:sha},canvas_hash:sha};const layer=(n:number,operation:string)=>({id:id(n),enabled:true,opacity:1,blend:'normal' as const,operation,operation_version:'1',params:{artifact_id:id(n+10),sha256:sha},mask:{op:'ref' as const,mask:{artifact_id:id(n+20),sha256:sha},canvas_hash:sha}});return {schema_version:1,source,layers:[layer(3,'optical.lens'),layer(4,'color.render')]};}
it('snapshots valid layers and edits only the chosen layer',()=>{const stack=fixture();const changed=patchEditorLayer(stack,id(4),{opacity:.5,blend:'screen'});expect(changed.layers[1].opacity).toBe(.5);expect(stack.layers[1].opacity).toBe(1);changed.layers[0].params.sha256='b'.repeat(64);expect(stack.layers[0].params.sha256).toBe(sha);});
it('rejects geometry after photometric layers and wrong-canvas masks',()=>{const stack=fixture();expect(()=>moveEditorLayer(stack,id(3),1)).toThrow();stack.layers[0].mask={op:'ref',mask:{artifact_id:id(30),sha256:sha},canvas_hash:'b'.repeat(64)};expect(()=>readLayerStack(stack,stack.source)).toThrow();});
it('enforces mask depth and a shared node budget over the whole stack',()=>{const stack=fixture();let mask:MaskExpr=stack.layers[0].mask;for(let i=0;i<17;i++)mask={op:'invert',operand:mask};stack.layers[0].mask=mask;expect(()=>readLayerStack(stack,stack.source)).toThrow();const wide=fixture();const tree=(depth:number):MaskExpr=>depth===0?wide.layers[0].mask:{op:'union',left:tree(depth-1),right:tree(depth-1)};wide.layers[0].mask=tree(7);expect(()=>readLayerStack(wide,wide.source)).not.toThrow();wide.layers[1].mask={op:'invert',operand:wide.layers[1].mask};expect(()=>readLayerStack(wide,wide.source)).toThrow();});
it('rejects duplicate IDs, unknown patches and invalid opacity',()=>{const stack=fixture();expect(()=>patchEditorLayer(stack,id(4),{opacity:NaN})).toThrow();stack.layers[1].id=stack.layers[0].id;expect(()=>readLayerStack(stack,stack.source)).toThrow();});

it('verifies the pinned immutable layer document SHA before presentation',()=>{
 const stack=fixture(),ref={artifact_id:id(50),sha256:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(stack))))};
 expect(readLayerDocument(stack,ref,stack.source)).toEqual(stack);expect(()=>readLayerDocument(stack,{...ref,sha256:'b'.repeat(64)},stack.source)).toThrow();expect(()=>readLayerDocument({...stack,layers:[]},ref,stack.source)).toThrow();
});
