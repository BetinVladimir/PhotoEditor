import {describe,it,expect} from 'vitest';
import {editorRegionScope,regionGeometry,regionRenderedScale} from './region-preview-view';
import {RegionPreviewSession} from './region-preview-session';
import {createEditorState,applyEditorCommand,undoEditor} from './editor-store';
import type {EditorDraft} from './catalog-types';
const id='11111111-1111-4111-8111-111111111111';
const scope={origin:'https://photo.test',user:id,tenant:id,asset:id};
const full={x0:0,y0:0,x1:1,y1:1};
describe('Region view contracts',()=>{
 it('uses outward crop rounding and bounded output without upscaling',()=>{
  expect(regionGeometry(6000,4000,{x0:0.25,y0:0.25,x1:0.75,y1:0.75},1024)).toEqual({left:1500,top:1000,width:3000,height:2000,output_width:1024,output_height:682});
  expect(regionGeometry(101,53,{x0:0.101,y0:0.201,x1:0.109,y1:0.209},64)).toEqual({left:10,top:10,width:2,height:2,output_width:2,output_height:2});
  expect(regionGeometry(1,200000000,full,64).output_width).toBe(1);
 });
 it('never claims full source scale for a reduced image',()=>{
  const reduced=regionGeometry(6000,4000,full,1024);expect(regionRenderedScale(reduced)).toEqual({x:1024/6000,y:682/4000,sourcePixels:false});
  expect(regionRenderedScale(regionGeometry(640,480,full,1024))).toEqual({x:1,y:1,sourcePixels:true});
  expect(()=>regionRenderedScale({...reduced,output_width:6001})).toThrow();
 });
 it('rejects invalid canvas and region sizes',()=>{
  for(const [w,h] of [[0,1],[1,0],[200000001,1],[20000,20000],[1.5,1],[NaN,1]])expect(()=>regionGeometry(w,h,full,1024)).toThrow();
  expect(()=>regionGeometry(10,10,{...full,x1:0},64)).toThrow();expect(()=>regionGeometry(10,10,full,2048)).toThrow();
 });
 it('invalidates an in-flight ticket after a local edit and undo',async()=>{
  const initial=createEditorState({asset_version_id:id,source_sha256:'a'.repeat(64),revision:'1',params:{},mask_refs:[]} as EditorDraft);
  const edited=applyEditorCommand(initial,{params:{exposure:1}}),undone=undoEditor(edited);
  expect(editorRegionScope(scope,initial).revision).not.toBe(editorRegionScope(scope,undone).revision);
  let resolve!:(value:{run_id:string;workflow_id:string;state:'queued'})=>void;
  const session=new RegionPreviewSession(()=>new Promise(a=>{resolve=a;}));session.bind(editorRegionScope(scope,initial));
  const pending=session.request({version_id:id,recipe:{artifact_id:id,sha256:'b'.repeat(64)},rect:full,max_edge:1024},'retry');session.bind(editorRegionScope(scope,edited));resolve({run_id:id,workflow_id:id,state:'queued'});expect(await pending).toBeUndefined();
  for(const state of [{...initial,sourceSha:'c'.repeat(64)},{...initial,versionId:'other'},{...initial,draftRevision:2}])expect(editorRegionScope(scope,state).revision).not.toBe(editorRegionScope(scope,initial).revision);
 });
});
