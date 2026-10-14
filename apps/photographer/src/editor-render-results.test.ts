import {it,expect,vi} from 'vitest';
import {loadEditorRenderResults,readRenderImages,renderPresentationLabel} from './editor-render-results';
const parent='11111111-1111-1111-1111-111111111111',child='22222222-2222-2222-2222-222222222222';
const ticket=()=>({run_id:parent,state:'queued' as const,child_run_ids:[child]});
const status=()=>({id:child,kind:'v2',status:'completed',revision:'1',counts:{total:1,succeeded:1,failed:0,skipped:0,review:0},stage:'done',progress:1,errors:[]});
const image=()=>({artifact_id:'33333333-3333-3333-3333-333333333333',role:'derivative',sha256:'a'.repeat(64),bytes:42,media_type:'image/jpeg',url:'https://objects.example/image?versionId=v1&signature=abc',expires_at:'2099-01-01T00:00:00Z'});
it('uses only the owned child ticket and detaches encoded image metadata',async()=>{
 const owned=ticket(),out=image();const api={run:vi.fn(async()=>{owned.child_run_ids[0]=parent;return status();}),runOutputs:vi.fn(async()=>({items:[out]}))};
 const results=await loadEditorRenderResults(api,owned,new AbortController().signal);out.url='https://evil.example';
 expect(api.runOutputs.mock.calls.length).toBe(1);expect(results[0].run).toBe(child);expect(results[0].images[0].url).toContain('objects.example');
});
it('rejects another child and aborts before outputs after cancellation',async()=>{
 const outputs=vi.fn();await expect(loadEditorRenderResults({run:async()=>({...status(),id:parent}),runOutputs:outputs},ticket(),new AbortController().signal)).rejects.toThrow('RENDER_STATUS_INVALID');
 const controller=new AbortController();await expect(loadEditorRenderResults({run:async()=>{controller.abort();return status();},runOutputs:outputs},ticket(),controller.signal)).rejects.toThrow('RENDER_RESULTS_ABORTED');expect(outputs).not.toHaveBeenCalled();
});
it('excludes expired or master artifacts and rejects malformed signed output metadata',()=>{
 expect(readRenderImages({items:[{...image(),role:'working_image'}]})).toEqual([]);
 expect(readRenderImages({items:[{...image(),expires_at:'2000-01-01T00:00:00Z'}]})).toEqual([]);
 for(const bad of [{...image(),url:'javascript:alert(1)'},{...image(),url:'http://objects.example/image?versionId=v1'},{...image(),url:'https://objects.example/image#fragment'},{...image(),url:'https://user:secret@objects.example/i?versionId=v1'},{...image(),url:'https://objects.example/image'},{...image(),url:'https://objects.example/image?versionId=v1&versionId=v2'},{...image(),bytes:1.5},{...image(),expires_at:'bad'},{...image(),sha256:'bad'}])expect(()=>readRenderImages({items:[bad]})).toThrow('RENDER_OUTPUT_INVALID');
 expect(()=>readRenderImages({items:[image(),image()]})).toThrow('RENDER_OUTPUT_INVALID');
 expect(readRenderImages({items:[{...image(),url:'http://localhost:9000/image?versionId=v1'}]})).toHaveLength(1);
 expect(readRenderImages({items:[{...image(),url:'https://api.photo.test/api/v1/media/run-output?token=sealed&versionId=v1'}]})).toHaveLength(1);
});

it('formats presentation counters through the selected locale catalog',async()=>{
 const ru=(await import('../../../locales/ru/common.json')).default;
 const bg=(await import('../../../locales/bg/common.json')).default;
 const frame={run:child,status:'completed',images:[],presentation:{total:3,pending:0,ready:1,failed:1,cancelled:0,unavailable:1}};
 expect(renderPresentationLabel(frame,key=>ru[key])).toContain('Готово для просмотра: 1/3');
 expect(renderPresentationLabel(frame,key=>bg[key])).toContain('Готови за преглед: 1/3');
 expect(renderPresentationLabel(frame,key=>ru[key])).not.toContain('{');
});
