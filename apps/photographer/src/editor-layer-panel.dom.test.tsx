// @vitest-environment jsdom
import {useState} from 'react';
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import en from '../../../locales/en/common.json';
import {createEditorState,currentEditorSnapshot,undoEditor,type EditorState} from './editor-store';
import {setEditorLayerStack} from './editor-layer-commands';
import type {EditorLayerStack} from './editor-layers';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {EditorLayerPanel} from './editor-layer-panel';
afterEach(cleanup);
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
const binding={asset_version_id:id(1),source:{artifact_id:id(2),sha256:sha},canvas_hash:sha};
const layer=(n:number,operation:string)=>({id:id(n),enabled:true,opacity:1,blend:'normal' as const,operation,operation_version:'1',params:{artifact_id:id(n+10),sha256:sha},mask:{op:'ref' as const,mask:{artifact_id:id(n+20),sha256:sha},canvas_hash:sha}});
const stack:EditorLayerStack={schema_version:1,source:binding,layers:[layer(3,'optical.lens'),layer(4,'color.render')]};
it('routes controls through shared history and refuses an invalid geometry move',()=>{
 let current!:EditorState;
 function Harness(){const [state,setState]=useState(()=>setEditorLayerStack(createEditorState({asset_version_id:id(1),source_sha256:sha,revision:'1',params:{schema_version:1,modules:{}},mask_refs:[]}),binding,stack));current=state;return <EditorLayerPanel state={state} binding={binding} onChange={setState}/>;}
 render(<Harness/>);fireEvent.click(screen.getAllByLabelText(en.layers_opacity_down)[1]);expect(currentEditorSnapshot(current).layerStack?.layers[1].opacity).toBe(.9);expect(currentEditorSnapshot(undoEditor(current)).layerStack?.layers[1].opacity).toBe(1);
 const before=current;fireEvent.click(screen.getAllByText(en.layers_move_down)[0]);expect(screen.getByText(en.layers_invalid)).toBeDefined();expect(current).toBe(before);
 fireEvent.click(screen.getAllByText(en.layers_disable)[1]);expect(currentEditorSnapshot(current).layerStack?.layers[1].enabled).toBe(false);
});
