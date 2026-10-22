// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,render,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {APIError} from '@photoeditor/api-client';
import en from '../../../locales/en/common.json';
const mocks=vi.hoisted(()=>({api:{draft:vi.fn(),me:vi.fn(),mediaLinks:vi.fn(),capabilities:vi.fn(),collection:vi.fn(),styles:vi.fn(),styleVersions:vi.fn(),saveDraft:vi.fn(),createOverride:vi.fn(),saveEdits:vi.fn(),submitSaveEdits:vi.fn(),resolveSaveEdits:vi.fn(),preview:vi.fn(),v2Draft:vi.fn(),v2Binding:vi.fn(),v2Document:vi.fn(),v2Layers:vi.fn(),saveV2Draft:vi.fn(),bridgeEditorRecipe:vi.fn(),freezeRenderDraft:vi.fn(),editorCapabilities:vi.fn(),generate:vi.fn(),abandonGeneration:vi.fn(),run:vi.fn(),runOutputs:vi.fn()},query:{tenant:'11111111-1111-4111-8111-111111111111',asset:'44444444-4444-4444-8444-444444444444',version:'22222222-2222-4222-8222-222222222222',collection:'',item:''},auth:{origin:'https://editor-test',client:()=>({})},storage:new Map<string,string>()}));
const app=vi.hoisted(()=>({state:'active',listeners:new Set<(state:string)=>void>()}));
const retainedSaveEdits=vi.hoisted(()=>({acknowledgements:[] as (()=>void)[]}));
const effectsAPI=Object.assign(mocks.api,{queueEditorEffects:vi.fn(),editorEffectsStatus:vi.fn()});
vi.mock('react-native',async()=>{const web=await import('react-native-web');return {...web,AppState:{get currentState(){return app.state},addEventListener:(_event:string,listener:(state:string)=>void)=>{app.listeners.add(listener);return{remove:()=>app.listeners.delete(listener)}}}};});
vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>();return {...paper,Button:(props:React.ComponentProps<typeof paper.Button>)=>{if(props.children==='Start another saved collection')retainedSaveEdits.acknowledgements.push(props.onPress as ()=>void);return <paper.Button {...props}/>;}};});
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>mocks.query,useRouter:()=>({push:vi.fn()})}));
vi.mock('./auth-context',()=>({useAuth:()=>mocks.auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class{constructor(){return mocks.api}}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>mocks.storage.get(k)??null,setItem:async(k:string,v:string)=>{mocks.storage.set(k,v)},removeItem:async(k:string)=>{mocks.storage.delete(k)},getAllKeys:async()=>[...mocks.storage.keys()]}}));
vi.mock('./editor-panels',()=>({EditorPanels:()=>null}));
vi.mock('./mask-canvas',()=>({MaskCanvas:()=>null}));
vi.mock('./compare-canvas',()=>({CompareCanvas:()=>null}));
import Editor from '../screens/editor/[version]';
const base={asset_version_id:'22222222-2222-4222-8222-222222222222',source_sha256:'a'.repeat(64),revision:'4',params:{schema_version:1,modules:{strength:1}},mask_refs:[]};
async function readyButton(name:string){
 await waitFor(()=>{const button=screen.getByRole('button',{name});expect(button.hasAttribute('disabled')).toBe(false);expectEnabled(button);});
 return screen.getByRole('button',{name});
}
async function clickReady(name:string){await clickWhenEnabled(await readyButton(name));}
async function clickSaveDraft(){await clickReady('Save draft');}
afterEach(cleanup);
beforeEach(()=>{
 retainedSaveEdits.acknowledgements.length=0;app.state='active';vi.resetAllMocks();mocks.storage.clear();mocks.query.asset='44444444-4444-4444-8444-444444444444';mocks.query.version=base.asset_version_id;mocks.query.collection='';mocks.query.item='';
 mocks.api.draft.mockResolvedValue(base);mocks.api.me.mockResolvedValue({id:'33333333-3333-4333-8333-333333333333'});
 mocks.api.editorCapabilities.mockResolvedValue({available:true});
 mocks.api.mediaLinks.mockResolvedValue({items:[]});mocks.api.capabilities.mockResolvedValue({operations:[]});
});
it('compares a CAS conflict and retries local edits with the fetched revision',async()=>{
 const server={...base,revision:'5',params:{schema_version:1,modules:{strength:.8}}};
 mocks.api.draft.mockResolvedValueOnce(base).mockResolvedValue(server);
 mocks.api.saveDraft.mockRejectedValueOnce(new APIError(409,'REVISION_CONFLICT')).mockImplementationOnce(async(_asset,payload)=>({...payload,revision:'6'}));
 render(<Editor/>);
 const input=screen.getByLabelText('Editor recipe');
 await waitFor(()=>expect((input as HTMLTextAreaElement).value).toContain('strength'));
 const local={schema_version:1,modules:{strength:.4}};
 fireEvent.change(input,{target:{value:JSON.stringify(local)}});
 await clickSaveDraft();
 await screen.findByText(en.editor_conflict_title);
 expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1);
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:en.editor_conflict_keep_local})));
 await clickWhenEnabled(screen.getByRole('button',{name:en.editor_conflict_keep_local}));
 expect((input as HTMLTextAreaElement).value).toBe(JSON.stringify(local));
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(2));
 expect(mocks.api.saveDraft.mock.calls[1][1]).toMatchObject({revision:'5',params:local});
 await waitFor(()=>expect(screen.queryByText(en.editor_conflict_title)).toBeNull());
});

it('keeps a newer recipe while a save response is pending',async()=>{
 let finish!:(value:unknown)=>void;
 mocks.api.saveDraft.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 render(<Editor/>);
 const input=screen.getByLabelText('Editor recipe');
 await waitFor(()=>expect((input as HTMLTextAreaElement).value).toContain('strength'));
 const submitted={schema_version:1,modules:{strength:.4}},newer={schema_version:1,modules:{strength:.2}};
 fireEvent.change(input,{target:{value:JSON.stringify(submitted)}});
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 fireEvent.change(input,{target:{value:JSON.stringify(newer)}});
 await act(async()=>{finish({...base,params:submitted,revision:'5'})});
 expect((input as HTMLTextAreaElement).value).toBe(JSON.stringify(newer));
 await screen.findByText('The submitted revision was saved. Your newer local edits remain unsaved.');
 await waitFor(()=>expect([...mocks.storage.values()].some(raw=>{
  const cached=JSON.parse(raw);return cached.base.revision==='5'&&cached.local.params.modules.strength===.2;
 })).toBe(true));
});

it('ignores a late legacy save acknowledgement after switching photos',async()=>{
 let finish!:(value:unknown)=>void;
 const next={...base,asset_version_id:'44444444-4444-4444-8444-444444444444',source_sha256:'b'.repeat(64),params:{schema_version:1,modules:{strength:.7}}};
 mocks.api.draft.mockImplementation(async asset=>asset==='55555555-5555-4555-8555-555555555555'?next:base);
 mocks.api.saveDraft.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<Editor/>);
 await waitFor(()=>expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('strength'));
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 mocks.query.asset='55555555-5555-4555-8555-555555555555';mocks.query.version=next.asset_version_id;
 view.rerender(<Editor/>);
 await waitFor(()=>expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('0.7'));
 await act(async()=>{finish({...base,revision:'5'})});
 expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('0.7');
 expect(screen.queryByText('Draft could not be saved. Check the recipe and reload if its revision changed.')).toBeNull();
});

it('keeps collection styles scoped when an older request finishes late',async()=>{
 let finishOld!:(value:unknown)=>void;
 mocks.query.collection='old';
 mocks.api.collection.mockImplementation(async id=>id==='old'?await new Promise(resolve=>{finishOld=resolve}):{project_id:'new-project'});
 mocks.api.styles.mockImplementation(async project=>({items:[{id:project,name:project}]}));
 const view=render(<Editor/>);
 await waitFor(()=>expect(mocks.api.collection).toHaveBeenCalledWith('old',expect.any(AbortSignal)));
 mocks.query.collection='88888888-8888-4888-8888-888888888888';view.rerender(<Editor/>);
 await screen.findByText('new-project');
 await act(async()=>{finishOld({project_id:'old-project'})});
 expect(screen.queryByText('old-project')).toBeNull();
 expect(screen.getByText('new-project')).toBeTruthy();
});

it('keeps the latest style version choice within one collection',async()=>{
 let finishOld!:(value:unknown)=>void;
 mocks.query.collection='current';
 mocks.api.collection.mockResolvedValue({project_id:'project'});
 mocks.api.styles.mockResolvedValue({items:[{id:'first',name:'First style'},{id:'second',name:'Second style'}]});
 mocks.api.styleVersions.mockImplementation(async id=>id==='first'?await new Promise(resolve=>{finishOld=resolve}):{items:[{id:'new-version',version_no:'2',approval_status:'accepted',export_artifact_id:'artifact'}]});
 render(<Editor/>);
 await clickWhenEnabled(await screen.findByText('First style'));
 await clickWhenEnabled(screen.getByText('Second style'));
 await screen.findByText('Create V2 draft · style version 2');
 await act(async()=>{finishOld({items:[{id:'old-version',version_no:'1',approval_status:'accepted',export_artifact_id:'artifact'}]})});
 expect(screen.queryByText('Create V2 draft · style version 1')).toBeNull();
 expect(screen.getByText('Create V2 draft · style version 2')).toBeTruthy();
});

it('restores cached local edits against an original base and compares newer server revision',async()=>{
 const local={schema_version:1,modules:{strength:.3}};
 const key='photo.editor-draft.v1:'+JSON.stringify([mocks.auth.origin,'33333333-3333-4333-8333-333333333333','11111111-1111-4111-8111-111111111111',base.asset_version_id]);
 mocks.storage.set(key,JSON.stringify({schema_version:1,base,local:{params:local,maskRefs:[]},saved_at:Date.now()}));
 mocks.api.draft.mockResolvedValue({...base,revision:'9'});
 render(<Editor/>);
 await screen.findByText(en.editor_conflict_title);
 expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('0.3');
 expect(mocks.api.saveDraft).not.toHaveBeenCalled();
 await clickWhenEnabled(screen.getByRole('button',{name:en.editor_conflict_keep_local}));
 mocks.api.saveDraft.mockImplementationOnce(async(_asset,payload)=>({...payload,revision:'10'}));
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 expect(mocks.api.saveDraft.mock.calls[0][1]).toMatchObject({revision:'9',params:local});
});
it('refuses a cached snapshot from a changed source without a server write',async()=>{
 const key='photo.editor-draft.v1:'+JSON.stringify([mocks.auth.origin,'33333333-3333-4333-8333-333333333333','11111111-1111-4111-8111-111111111111',base.asset_version_id]);
 mocks.storage.set(key,JSON.stringify({schema_version:1,base:{...base,source_sha256:'b'.repeat(64)},local:{params:{schema_version:1,modules:{strength:.1}},maskRefs:[]},saved_at:Date.now()}));
 render(<Editor/>);
 await screen.findByText(en.editor_conflict_source_changed);
 expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('"strength": 1');
 expect(screen.queryByText(en.editor_conflict_title)).toBeNull();
 expect(mocks.api.saveDraft).not.toHaveBeenCalled();
});

const layerId=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const layerDigest=(v:unknown)=>bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(v))));
function configureLayers(){
 const binding={asset_version_id:base.asset_version_id,source:{artifact_id:layerId(1),sha256:base.source_sha256},canvas_hash:base.source_sha256};
 const recipe={schema_version:3,source:binding,base_style:{artifact_id:layerId(2),sha256:base.source_sha256},session_corrections:null,learning_version_id:null,overrides:[],layer_stack:null,renderer_version:'v2.2',parameter_delta:{ev:0,log_r:0,log_g:0,log_b:0,saturation_delta:0,retouch_delta:0}};
 const draft={id:layerId(3),revision:'2',document:{artifact_id:layerId(4),sha256:layerDigest(recipe)},data:{id:layerId(3),revision:'2',resource_type:'v2_document_streams',stream_type:'draft',asset_version_id:base.asset_version_id,current_document_id:layerId(5)}};
 mocks.api.v2Draft.mockResolvedValue(draft);mocks.api.v2Binding.mockResolvedValue(binding);mocks.api.v2Document.mockResolvedValue(recipe);
 return {recipe,draft,commit:(layers:unknown)=>{
  const committed={...recipe,layer_stack:{artifact_id:layerId(6),sha256:layerDigest(layers)}};
  mocks.api.v2Document.mockResolvedValue(committed);
  return {...draft,revision:'3',document:{artifact_id:layerId(7),sha256:layerDigest(committed)},data:{...draft.data,revision:'3',current_document_id:layerId(8)}};
 }};
}
async function openLayers(){
 await screen.findByText(/revision 4/);
 await clickReady(en.layers_load);
 await readyButton(en.layers_save);
}
it('retries the same V2 layer request and key after losing the response',async()=>{
 const fixture=configureLayers();mocks.api.saveV2Draft.mockRejectedValueOnce(new Error('offline')).mockImplementationOnce(async(_asset,_revision,_recipe,layers)=>fixture.commit(layers));
 render(<Editor/>);await openLayers();
 await clickWhenEnabled(screen.getByRole('button',{name:en.layers_save}));await screen.findByText(en.layers_save_retry);
 expect(screen.getByRole('button',{name:en.layers_load}).getAttribute('aria-disabled')).toBe('true');
 await waitFor(()=>expect(screen.getByRole('button',{name:en.layers_retry_save}).hasAttribute('disabled')).toBe(false));
 await clickWhenEnabled(screen.getByRole('button',{name:en.layers_retry_save}));await screen.findByText(en.layers_saved);
 expect(mocks.api.saveV2Draft).toHaveBeenCalledTimes(2);
 expect(mocks.api.saveV2Draft.mock.calls[1].slice(0,5)).toEqual(mocks.api.saveV2Draft.mock.calls[0].slice(0,5));
 expect(screen.getByText(/revision 4/)).toBeTruthy();
});
it('preserves recipe edits made while the V2 layer save is pending',async()=>{
 const fixture=configureLayers();let finish!:(value:unknown)=>void;
 mocks.api.saveV2Draft.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 render(<Editor/>);await openLayers();await clickWhenEnabled(screen.getByRole('button',{name:en.layers_save}));
 await waitFor(()=>expect(mocks.api.saveV2Draft).toHaveBeenCalledTimes(1));
 const local=JSON.stringify({schema_version:1,modules:{strength:.23}});fireEvent.change(screen.getByLabelText('Editor recipe'),{target:{value:local}});
 await act(async()=>finish(fixture.commit(mocks.api.saveV2Draft.mock.calls[0][3])));
 await screen.findByText(en.layers_saved);expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toBe(local);
});
it('ignores a late V2 save acknowledgement after switching photos',async()=>{
 const fixture=configureLayers();let finish!:(value:unknown)=>void;
 mocks.api.saveV2Draft.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<Editor/>);await openLayers();await clickWhenEnabled(screen.getByRole('button',{name:en.layers_save}));
 await waitFor(()=>expect(mocks.api.saveV2Draft).toHaveBeenCalledTimes(1));
 const signal=mocks.api.saveV2Draft.mock.calls[0][5] as AbortSignal;
 mocks.query.asset='66666666-6666-4666-8666-666666666666';mocks.query.version=layerId(10);mocks.api.draft.mockResolvedValue({...base,asset_version_id:layerId(10)});
 view.rerender(<Editor/>);await waitFor(()=>expect(signal.aborted).toBe(true));
 await act(async()=>finish(fixture.commit(mocks.api.saveV2Draft.mock.calls[0][3])));
 expect(screen.queryByText(en.layers_saved)).toBeNull();expect(screen.queryByRole('button',{name:en.layers_save})).toBeNull();
});
it('requires refresh rather than another write after an acknowledged but unreadable commit',async()=>{
 const fixture=configureLayers();
 mocks.api.saveV2Draft.mockImplementationOnce(async(_asset,_revision,_recipe,layers)=>{
  const saved=fixture.commit(layers);mocks.api.v2Document.mockRejectedValueOnce(new Error('document offline'));return saved;
 });
 render(<Editor/>);await openLayers();await clickWhenEnabled(screen.getByRole('button',{name:en.layers_save}));
 await screen.findByText(en.layers_saved_refresh);
 expect(screen.queryByRole('button',{name:en.layers_retry_save})).toBeNull();
 expect(screen.queryByRole('button',{name:en.layers_save})).toBeNull();
 expectEnabled(screen.getByRole('button',{name:en.layers_load}));
 expect(mocks.api.saveV2Draft).toHaveBeenCalledTimes(1);
});
it('clears the stale V2 baseline after a CAS rejection',async()=>{
 configureLayers();mocks.api.saveV2Draft.mockRejectedValueOnce(new APIError(409,'REVISION_CONFLICT'));
 render(<Editor/>);await openLayers();await clickWhenEnabled(screen.getByRole('button',{name:en.layers_save}));
 await screen.findByText(en.layers_save_reload);
 expect(screen.queryByRole('button',{name:en.layers_save})).toBeNull();
 expect(screen.queryByRole('button',{name:en.layers_retry_save})).toBeNull();
 expect(mocks.api.saveV2Draft).toHaveBeenCalledTimes(1);
});

async function seedGenerationJournal(accepted:boolean){
 const {EditorGenerationAttempt}=await import('./editor-generation-attempt');
 const {EditorGenerationStore}=await import('./editor-generation-store');
 const storage={getItem:async(k:string)=>mocks.storage.get(k)??null,setItem:async(k:string,v:string)=>{mocks.storage.set(k,v)},removeItem:async(k:string)=>{mocks.storage.delete(k)}};
 const journal=new EditorGenerationStore(storage,mocks.auth.origin,'33333333-3333-4333-8333-333333333333',mocks.query.tenant,mocks.query.asset,base.asset_version_id,async(_origin,action)=>action());
 const attempt=new EditorGenerationAttempt(base,{artifact:{artifact_id:'66666666-6666-4666-8666-666666666666',sha256:'b'.repeat(64)},canvas:{width:10,height:10},transform_chain:{},selector:'object',confidence:1,protected:false},'original prompt',7);
 const receipt={run_id:'77777777-7777-4777-8777-777777777777',status:'queued'};
 if(accepted)await attempt.submit(vi.fn().mockResolvedValue(receipt));await journal.write(attempt.snapshot);return {journal,attempt,receipt};
}
it('restores accepted generation and retries output read without save or generation when capability is unavailable',async()=>{
 const {journal,receipt}=await seedGenerationJournal(true);
 mocks.api.run.mockResolvedValue({status:'completed'});mocks.api.runOutputs.mockRejectedValueOnce(new Error('read lost')).mockResolvedValue({items:[editorImage('https://images.example/result.jpg')]});
 render(<Editor/>);const retry=await screen.findByText('Retry generation');
 await waitFor(()=>expectEnabled(retry));await clickWhenEnabled(retry);
 await screen.findByText('Generation failed. Check the mask, prompt, and qualified SD 1.5 worker.');
 await waitFor(()=>expectEnabled(retry));await clickWhenEnabled(retry);
 await waitFor(()=>expect(mocks.storage.has(journal.key)).toBe(false));expect(mocks.api.generate).not.toHaveBeenCalled();expect(mocks.api.saveDraft).not.toHaveBeenCalled();expect(mocks.api.run).toHaveBeenCalledWith(receipt.run_id,expect.any(AbortSignal));
});
it('restores uncertain generation under its original body and key without saving current draft',async()=>{
 const {journal,attempt,receipt}=await seedGenerationJournal(false);
 mocks.api.capabilities.mockResolvedValue({operations:[{operation:'image.generate',status:'ready',available:true,supported_backends:['cuda'],available_backends:['cuda']}]});
 mocks.api.generate.mockResolvedValue(receipt);mocks.api.run.mockResolvedValue({status:'completed'});mocks.api.runOutputs.mockResolvedValue({items:[editorImage('https://images.example/result.jpg')]});
 render(<Editor/>);const retry=await screen.findByText('Retry generation');await waitFor(()=>expectEnabled(retry));await clickWhenEnabled(retry);
 await waitFor(()=>expect(mocks.storage.has(journal.key)).toBe(false));expect(mocks.api.saveDraft).not.toHaveBeenCalled();expect(mocks.api.generate).toHaveBeenCalledTimes(1);
 expect(mocks.api.generate.mock.calls[0].slice(2,6)).toEqual(['original prompt',7,1,attempt.key]);expect(mocks.api.generate.mock.calls[0][6]).toBeInstanceOf(AbortSignal);
});

function generationAppState(state:string){act(()=>{app.state=state;app.listeners.forEach(listener=>listener(state))})}
it('keeps restored generation disabled in initial background and resumes journal on foreground',async()=>{
 await seedGenerationJournal(true);app.state='background';render(<Editor/>);await act(async()=>{});
 expect(screen.queryByText('Retry generation')).toBeNull();expect(mocks.api.generate).not.toHaveBeenCalled();expect(mocks.api.run).not.toHaveBeenCalled();
 generationAppState('active');await screen.findByText('Retry generation');expect(mocks.api.generate).not.toHaveBeenCalled();
});
it('aborts hung generation result read in background and ignores its late response after successful recovery',async()=>{
 const {journal}=await seedGenerationJournal(true);let finish!:(value:unknown)=>void;
 mocks.api.run.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue({status:'completed'});mocks.api.runOutputs.mockResolvedValue({items:[editorImage('https://images.example/result.jpg')]});
 render(<Editor/>);await clickReady('Retry generation');await waitFor(()=>expect(mocks.api.run).toHaveBeenCalledOnce());const signal=mocks.api.run.mock.calls[0][1] as AbortSignal;
 generationAppState('background');expect(signal.aborted).toBe(true);generationAppState('active');
 await clickReady('Retry generation');
 await waitFor(()=>expect(mocks.storage.has(journal.key)).toBe(false));await act(async()=>finish({status:'completed'}));
 expect(mocks.api.runOutputs).toHaveBeenCalledTimes(1);expect(mocks.api.generate).not.toHaveBeenCalled();expect(mocks.api.saveDraft).not.toHaveBeenCalled();
});
it('aborts generation output read on source replacement and ignores old accepted result',async()=>{
 await seedGenerationJournal(true);let finish!:(value:unknown)=>void;mocks.api.run.mockImplementation(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<Editor/>);await clickReady('Retry generation');await waitFor(()=>expect(mocks.api.run).toHaveBeenCalledOnce());const signal=mocks.api.run.mock.calls[0][1] as AbortSignal;
 mocks.query.asset='88888888-8888-4888-8888-888888888888';mocks.query.version='99999999-9999-4999-8999-999999999999';mocks.api.draft.mockResolvedValue({...base,asset_version_id:mocks.query.version});view.rerender(<Editor/>);
 expect(signal.aborted).toBe(true);await act(async()=>finish({status:'completed'}));expect(mocks.api.runOutputs).not.toHaveBeenCalled();expect(mocks.api.generate).not.toHaveBeenCalled();expect(screen.queryByText('Retry generation')).toBeNull();
});

it('recovers uncertain generation without capability using replay-only under original key',async()=>{
 const {journal,attempt,receipt}=await seedGenerationJournal(false);
 mocks.api.generate.mockResolvedValue(receipt);mocks.api.run.mockResolvedValue({status:'completed'});mocks.api.runOutputs.mockResolvedValue({items:[editorImage('https://images.example/result.jpg')]});
 render(<Editor/>);const retry=await screen.findByText('Retry generation');await clickWhenEnabled(retry);
 await waitFor(()=>expect(mocks.storage.has(journal.key)).toBe(false));expect(mocks.api.saveDraft).not.toHaveBeenCalled();expect(mocks.api.generate).toHaveBeenCalledTimes(1);expect(mocks.api.generate.mock.calls[0].slice(2,6)).toEqual(['original prompt',7,1,attempt.key]);expect(mocks.api.generate.mock.calls[0][7]).toBe(true);
});

it.each(['failed','cancelled'])('acknowledges authoritative %s generation without resubmitting',async status=>{
 const {journal}=await seedGenerationJournal(true);mocks.api.run.mockResolvedValue({status});
 render(<Editor/>);await clickReady('Retry generation');
 const acknowledge=await screen.findByText('Acknowledge failed generation');expect(mocks.storage.has(journal.key)).toBe(true);
 await waitFor(()=>expectEnabled(acknowledge));await clickWhenEnabled(acknowledge);
 await waitFor(()=>expect(mocks.storage.has(journal.key)).toBe(false));expect(mocks.api.run).toHaveBeenCalledTimes(2);
 expect(mocks.api.generate).not.toHaveBeenCalled();expect(mocks.api.saveDraft).not.toHaveBeenCalled();expect(mocks.api.runOutputs).not.toHaveBeenCalled();
 expect(screen.queryByText('Acknowledge failed generation')).toBeNull();expect(screen.getByText('Confirm and generate candidate')).toBeTruthy();
});
it('keeps accepted generation after acknowledgement loses access',async()=>{
 const {journal}=await seedGenerationJournal(true);mocks.api.run.mockResolvedValueOnce({status:'failed'}).mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 render(<Editor/>);await clickReady('Retry generation');const acknowledge=await screen.findByText('Acknowledge failed generation');
 await waitFor(()=>expectEnabled(acknowledge));await clickWhenEnabled(acknowledge);
 await screen.findByText('Editor access is no longer available. Reopen this photo after signing in or restoring access.');
 expect(mocks.storage.has(journal.key)).toBe(true);expect(mocks.api.generate).not.toHaveBeenCalled();
});
it('does not expose acknowledgement for an uncertain outcome read',async()=>{
 const {journal}=await seedGenerationJournal(true);mocks.api.run.mockRejectedValue(new Error('connection lost'));
 render(<Editor/>);await clickReady('Retry generation');await screen.findByText('Generation failed. Check the mask, prompt, and qualified SD 1.5 worker.');
 expect(screen.queryByText('Acknowledge failed generation')).toBeNull();expect(mocks.storage.has(journal.key)).toBe(true);
});
it('preserves usable variants after generation completes with partial errors',async()=>{
 const {journal}=await seedGenerationJournal(true);mocks.api.run.mockResolvedValue({status:'completed_with_errors'});mocks.api.runOutputs.mockResolvedValue({items:[editorImage('https://images.example/partial.jpg')]});
 render(<Editor/>);await clickReady('Retry generation');await waitFor(()=>expect(mocks.storage.has(journal.key)).toBe(false));
 expect(screen.queryByText('Acknowledge failed generation')).toBeNull();expect(mocks.api.runOutputs).toHaveBeenCalledOnce();expect(mocks.api.generate).not.toHaveBeenCalled();
});
it('rechecks empty partial results before acknowledging a generation failure',async()=>{
 const {journal}=await seedGenerationJournal(true);mocks.api.run.mockResolvedValue({status:'completed_with_errors'});mocks.api.runOutputs.mockResolvedValue({items:[]});
 render(<Editor/>);await clickReady('Retry generation');const acknowledge=await screen.findByText('Acknowledge failed generation');
 await waitFor(()=>expectEnabled(acknowledge));await clickWhenEnabled(acknowledge);
 await waitFor(()=>expect(mocks.storage.has(journal.key)).toBe(false));expect(mocks.api.runOutputs).toHaveBeenCalledTimes(2);expect(mocks.api.generate).not.toHaveBeenCalled();
});
it('does not discard a partial result that becomes available during acknowledgement',async()=>{
 const {journal}=await seedGenerationJournal(true);mocks.api.run.mockResolvedValue({status:'completed_with_errors'});mocks.api.runOutputs.mockResolvedValueOnce({items:[]}).mockResolvedValue({items:[editorImage('https://images.example/available.jpg')]});
 render(<Editor/>);await clickReady('Retry generation');const acknowledge=await screen.findByText('Acknowledge failed generation');
 await waitFor(()=>expectEnabled(acknowledge));await clickWhenEnabled(acknowledge);
 await screen.findByText('The generation outcome could not be confirmed. Your saved request has been kept.');expect(mocks.storage.has(journal.key)).toBe(true);
});

it('ignores a late terminal acknowledgement after source replacement',async()=>{
 const {journal}=await seedGenerationJournal(true);let finish!:(value:unknown)=>void;
 mocks.api.run.mockResolvedValueOnce({status:'failed'}).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<Editor/>);await clickReady('Retry generation');const acknowledge=await screen.findByText('Acknowledge failed generation');
 await waitFor(()=>expectEnabled(acknowledge));await clickWhenEnabled(acknowledge);
 await waitFor(()=>expect(mocks.api.run).toHaveBeenCalledTimes(2));const signal=mocks.api.run.mock.calls[1][1] as AbortSignal;
 mocks.query.asset='88888888-8888-4888-8888-888888888888';mocks.query.version='99999999-9999-4999-8999-999999999999';mocks.api.draft.mockResolvedValue({...base,asset_version_id:mocks.query.version});view.rerender(<Editor/>);
 expect(signal.aborted).toBe(true);await act(async()=>finish({status:'failed'}));expect(mocks.storage.has(journal.key)).toBe(true);expect(screen.queryByText('Acknowledge failed generation')).toBeNull();
});

it('clears uncertain generation only after same-key server abandonment',async()=>{
 const {journal,attempt}=await seedGenerationJournal(false);mocks.api.abandonGeneration.mockResolvedValue({status:'abandoned',key:attempt.key});
 render(<Editor/>);await clickWhenEnabled(await screen.findByText('Abandon pending generation'));await waitFor(()=>expect(mocks.storage.has(journal.key)).toBe(false));
 expect(mocks.api.abandonGeneration.mock.calls[0].slice(2,6)).toEqual(['original prompt',7,1,attempt.key]);expect(mocks.api.generate).not.toHaveBeenCalled();expect(mocks.api.saveDraft).not.toHaveBeenCalled();
});
it('recovers already accepted generation when abandonment loses to admission',async()=>{
 const {journal,receipt}=await seedGenerationJournal(false);mocks.api.abandonGeneration.mockResolvedValue(receipt);
 render(<Editor/>);await clickWhenEnabled(await screen.findByText('Abandon pending generation'));await screen.findByText('This generation was already accepted. Retry to load its result.');
 expect(JSON.parse(mocks.storage.get(journal.key)!).receipt).toEqual(receipt);expect(screen.queryByText('Abandon pending generation')).toBeNull();expect(mocks.api.generate).not.toHaveBeenCalled();
});
it.each(['transport','wrong-key'])('keeps uncertain generation after %s abandonment response',async mode=>{
 const {journal}=await seedGenerationJournal(false);
 if(mode==='transport')mocks.api.abandonGeneration.mockRejectedValue(new Error('lost'));else mocks.api.abandonGeneration.mockResolvedValue({status:'abandoned',key:'foreign'});
 render(<Editor/>);await clickWhenEnabled(await screen.findByText('Abandon pending generation'));await screen.findByText('The pending generation could not be resolved. Your saved request has been kept.');
 expect(mocks.storage.has(journal.key)).toBe(true);expect(screen.getByText('Retry generation')).toBeTruthy();expect(mocks.api.generate).not.toHaveBeenCalled();
});

it('ignores late abandonment after background and keeps its original journal for reconciliation',async()=>{
 const {journal,attempt}=await seedGenerationJournal(false);let finish!:(value:unknown)=>void;mocks.api.abandonGeneration.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 render(<Editor/>);await clickWhenEnabled(await screen.findByText('Abandon pending generation'));await waitFor(()=>expect(mocks.api.abandonGeneration).toHaveBeenCalledOnce());
 const signal=mocks.api.abandonGeneration.mock.calls[0][6] as AbortSignal;generationAppState('background');expect(signal.aborted).toBe(true);
 await act(async()=>finish({status:'abandoned',key:attempt.key}));expect(mocks.storage.has(journal.key)).toBe(true);
 generationAppState('active');await screen.findByText('Retry generation');expect(mocks.api.generate).not.toHaveBeenCalled();
});


it('disables both preview scales when editor.preview is unavailable',async()=>{
 render(<Editor/>);
 await waitFor(()=>expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('strength'));
 for(const name of ['Render proxy preview','Render 100% preview']){
  const button=screen.getByRole('button',{name});
  expect(button.getAttribute('aria-disabled')).toBe('true');fireEvent.click(button);
 }
 expect(mocks.api.saveDraft).not.toHaveBeenCalled();expect(mocks.api.preview).not.toHaveBeenCalled();
});

it('rechecks preview availability after saving and keeps the saved draft on refusal',async()=>{
 let finish!:(value:unknown)=>void;
 mocks.api.capabilities.mockResolvedValue({operations:[{operation:'editor.preview',status:'ready',available:true,supported_backends:['cpu'],available_backends:['cpu']}]});
 mocks.api.saveDraft.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 render(<Editor/>);
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Render proxy preview'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Render proxy preview'}));
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 await act(async()=>{app.state='background';for(const listener of app.listeners)listener('background');});
 await act(async()=>{finish({...base,revision:'5'});});
 expect(mocks.api.preview).not.toHaveBeenCalled();
 expect(screen.queryByText('Preview could not be rendered. Check the recipe and worker availability.')).toBeNull();
 expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('strength');
});


it('does not create an override or collection after changing the collection during draft save',async()=>{
 let finish!:(value:unknown)=>void;
 mocks.query.collection='55555555-5555-4555-8555-555555555555';mocks.query.item='66666666-6666-4666-8666-666666666666';
 mocks.api.collection.mockImplementation(async id=>({id,project_id:'77777777-7777-4777-8777-777777777777',revision:'1',name:'Source'}));mocks.api.styles.mockResolvedValue({items:[]});
 mocks.api.saveDraft.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<Editor/>);
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Save edits as new collection'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Save edits as new collection'}));
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 mocks.query.collection='88888888-8888-4888-8888-888888888888';view.rerender(<Editor/>);
 await waitFor(()=>expect(mocks.api.collection).toHaveBeenCalledWith('88888888-8888-4888-8888-888888888888',expect.any(AbortSignal)));
 await act(async()=>{finish({...base,revision:'5'});});
 expect(mocks.api.createOverride).not.toHaveBeenCalled();expect(mocks.api.saveEdits).not.toHaveBeenCalled();
});


it('does not reload a conflict from an old editor route after a late save refusal',async()=>{
 let refuse!:(error:unknown)=>void;
 mocks.api.saveDraft.mockImplementationOnce(()=>new Promise((_resolve,reject)=>{refuse=reject}));
 const view=render(<Editor/>);
 await waitFor(()=>expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('strength'));
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 const next={...base,asset_version_id:'66666666-6666-4666-8666-666666666666',source_sha256:'c'.repeat(64)};
 mocks.api.draft.mockResolvedValue(next);mocks.query.asset='55555555-5555-4555-8555-555555555555';mocks.query.version=next.asset_version_id;
 view.rerender(<Editor/>);
 await waitFor(()=>expect(mocks.api.draft).toHaveBeenCalledTimes(2));
 await act(async()=>{refuse(new APIError(409,'REVISION_CONFLICT'));});
 expect(mocks.api.draft).toHaveBeenCalledTimes(2);
 expect(screen.queryByText(en.editor_conflict_title)).toBeNull();
});

it('aborts draft transport on background and refuses a late conflict reload after foreground returns',async()=>{
 let refuse!:(error:unknown)=>void;
 mocks.api.saveDraft.mockImplementationOnce(()=>new Promise((_resolve,reject)=>{refuse=reject}));
 render(<Editor/>);
 await waitFor(()=>expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('strength'));
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 const signal=mocks.api.saveDraft.mock.calls[0][2] as AbortSignal;
 expect(signal).toBeInstanceOf(AbortSignal);expect(signal.aborted).toBe(false);
 await act(async()=>{app.state='background';for(const listener of app.listeners)listener('background');});
 expect(signal.aborted).toBe(true);
 await act(async()=>{app.state='active';for(const listener of app.listeners)listener('active');});
 const reads=mocks.api.draft.mock.calls.length;
 await act(async()=>{refuse(new APIError(409,'REVISION_CONFLICT'));});
 expect(mocks.api.draft).toHaveBeenCalledTimes(reads);
 expect(screen.queryByText(en.editor_conflict_title)).toBeNull();
});

it('does not adopt a successful draft reply from an aborted action',async()=>{
 let finish!:(value:unknown)=>void;
 mocks.api.saveDraft.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockImplementationOnce(async(_asset,payload)=>({...payload,revision:'5'}));
 render(<Editor/>);
 await waitFor(()=>expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('strength'));
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 await act(async()=>{app.state='background';for(const listener of app.listeners)listener('background');});
 await act(async()=>{finish({...base,revision:'5'});});
 await act(async()=>{app.state='active';for(const listener of app.listeners)listener('active');});
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Save draft'})));
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(2));
 expect(mocks.api.saveDraft.mock.calls[1][1].revision).toBe('4');
 expect(mocks.api.saveDraft.mock.calls[1][2]).not.toBe(mocks.api.saveDraft.mock.calls[0][2]);
});

it('keeps a new foreground action locked when the old aborted action settles',async()=>{
 let first!:(value:unknown)=>void,second!:(value:unknown)=>void;
 mocks.api.saveDraft.mockImplementationOnce(()=>new Promise(resolve=>{first=resolve})).mockImplementationOnce(()=>new Promise(resolve=>{second=resolve}));
 render(<Editor/>);
 await waitFor(()=>expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('strength'));
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 await act(async()=>{app.state='background';for(const listener of app.listeners)listener('background');});
 await act(async()=>{app.state='active';for(const listener of app.listeners)listener('active');});
 // Start the replacement while the aborted transport is still unresolved.
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(2));
 await act(async()=>{first({...base,revision:'5'});});
 expect(screen.getByRole('button',{name:'Save draft'}).getAttribute('aria-disabled')).toBe('true');
 fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 expect(mocks.api.saveDraft).toHaveBeenCalledTimes(2);
 await act(async()=>{second({...base,revision:'5'});});
});

it('stops local waiting without allowing an aborted save failure to overwrite the replacement',async()=>{
 let rejectOld!:(error:Error)=>void,finishNew!:(value:unknown)=>void;
 mocks.api.saveDraft.mockImplementationOnce(()=>new Promise((_resolve,reject)=>{rejectOld=reject})).mockImplementationOnce(()=>new Promise(resolve=>{finishNew=resolve}));
 render(<Editor/>);
 await waitFor(()=>expect((screen.getByLabelText('Editor recipe') as HTMLTextAreaElement).value).toContain('strength'));
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 const oldSignal=mocks.api.saveDraft.mock.calls[0].at(-1) as AbortSignal;
 await clickWhenEnabled(screen.getByRole('button',{name:'Stop waiting'}));
 expect(oldSignal.aborted).toBe(true);
 await clickSaveDraft();
 await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(2));
 await act(async()=>{rejectOld(new Error('late transport failure'));});
 expect(screen.getByRole('button',{name:'Save draft'}).getAttribute('aria-disabled')).toBe('true');
 expect(screen.queryByText('Draft could not be saved. Check the recipe and reload if its revision changed.')).toBeNull();
 await act(async()=>{finishNew({...base,revision:'5'});});
});

it('loads all Editor style and version pages before presenting accepted choices',async()=>{
 mocks.query.collection='paged';mocks.api.collection.mockResolvedValue({id:'paged',project_id:'project'});
 mocks.api.styles.mockImplementation(async(_project,cursor)=>cursor?{items:[{id:'second',name:'Second page style'}]}:{items:[{id:'first',name:'First page style'}],next_cursor:'styles-next'});
 mocks.api.styleVersions.mockImplementation(async(_style,cursor)=>cursor?{items:[{id:'v2',version_no:2,approval_status:'accepted',export_artifact_id:'artifact2'}]}:{items:[{id:'v1',version_no:1,approval_status:'accepted',export_artifact_id:'artifact1'}],next_cursor:'versions-next'});
 render(<Editor/>);await clickWhenEnabled(await screen.findByText('Second page style'));
 await screen.findByText('Create V2 draft · style version 2');
 expect(screen.getByText('Create V2 draft · style version 1')).toBeTruthy();
 expect(mocks.api.styles).toHaveBeenCalledWith('project','styles-next',expect.any(AbortSignal));
 expect(mocks.api.styleVersions).toHaveBeenCalledWith('second','versions-next',expect.any(AbortSignal));
});
it('rejects a looping Editor catalogue without displaying a partial list',async()=>{
 mocks.query.collection='loop';mocks.api.collection.mockResolvedValue({id:'loop',project_id:'project'});
 mocks.api.styles.mockResolvedValue({items:[{id:'first',name:'Partial style'}],next_cursor:'again'});
 render(<Editor/>);await screen.findByText('Collection or its styles are unavailable.');
 expect(screen.queryByText('Partial style')).toBeNull();expect(mocks.api.styles).toHaveBeenCalledTimes(2);
});
it('aborts abandoned Editor style-version reads when switching choices',async()=>{
 let finish!:(value:unknown)=>void;mocks.query.collection='current';mocks.api.collection.mockResolvedValue({project_id:'project'});
 mocks.api.styles.mockResolvedValue({items:[{id:'first',name:'First style'},{id:'second',name:'Second style'}]});
 mocks.api.styleVersions.mockImplementation(async style=>style==='first'?await new Promise(resolve=>{finish=resolve}):{items:[{id:'v2',version_no:2,approval_status:'accepted',export_artifact_id:'artifact'}]});
 const view=render(<Editor/>);await clickWhenEnabled(await screen.findByText('First style'));await waitFor(()=>expect(mocks.api.styleVersions).toHaveBeenCalledTimes(1));
 const abandoned=mocks.api.styleVersions.mock.calls[0][2] as AbortSignal;await clickWhenEnabled(screen.getByText('Second style'));expect(abandoned.aborted).toBe(true);
 await screen.findByText('Create V2 draft · style version 2');
 await act(async()=>{finish({items:[{id:'v1',version_no:1,approval_status:'accepted',export_artifact_id:'artifact'}]})});expect(screen.queryByText('Create V2 draft · style version 1')).toBeNull();
 const current=mocks.api.styleVersions.mock.calls[1][2] as AbortSignal;view.unmount();expect(current.aborted).toBe(true);
});

it.each([401,403])('does not install an editable draft when principal lookup returns %s',async status=>{
 mocks.api.me.mockRejectedValue(new APIError(status,status===401?'UNAUTHORIZED':'FORBIDDEN'));
 render(<Editor/>);await screen.findByText('Editor access is no longer available. Reopen this photo after signing in or restoring access.');
 expect(screen.queryByRole('button',{name:'Save draft'})).toBeNull();expect(mocks.api.saveDraft).not.toHaveBeenCalled();
 expect(mocks.storage.size).toBe(0);
});

it('discards source-bound generation prompt and seed on source replacement',async()=>{
 const view=render(<Editor/>);fireEvent.change(screen.getByLabelText('Generation prompt'),{target:{value:'Old source only'}});fireEvent.change(screen.getByLabelText('Generation seed'),{target:{value:'99'}});
 mocks.query.asset='88888888-8888-4888-8888-888888888888';mocks.query.version='99999999-9999-4999-8999-999999999999';mocks.api.draft.mockResolvedValue({...base,asset_version_id:mocks.query.version});view.rerender(<Editor/>);
 expect((screen.getByLabelText('Generation prompt') as HTMLInputElement).value).toBe('');expect((screen.getByLabelText('Generation seed') as HTMLInputElement).value).toBe('1');
});

it('tears down the Editor after mutation authorization is revoked without clearing journals',async()=>{
 mocks.api.saveDraft.mockRejectedValue(new APIError(403,'FORBIDDEN'));const view=render(<Editor/>);
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Save draft'})));await clickSaveDraft();
 await screen.findByText('Editor access is no longer available. Reopen this photo after signing in or restoring access.');
 expect(screen.queryByLabelText('Editor recipe')).toBeNull();expect(screen.queryByLabelText('Generation prompt')).toBeNull();view.unmount();
});

function editorImage(url:string){return {artifact_id:'11111111-1111-4111-8111-111111111112',role:'derivative',sha256:'b'.repeat(64),bytes:100,media_type:'image/jpeg',url:url+'?versionId=pinned',expires_at:'2099-01-01T00:00:00Z'}}
it('keeps an accepted generation journal when output metadata is not pinned',async()=>{
 const {journal}=await seedGenerationJournal(true);mocks.api.run.mockResolvedValue({status:'completed'});mocks.api.runOutputs.mockResolvedValue({items:[{...editorImage('https://images.example/result.jpg'),url:'https://images.example/unpinned.jpg'}]});
 render(<Editor/>);await clickReady('Retry generation');await screen.findByText('Generation failed. Check the mask, prompt, and qualified SD 1.5 worker.');expect(mocks.storage.has(journal.key)).toBe(true);
});

it('serializes duplicate draft clicks while the same mutation is pending',async()=>{
 let finish!:(value:unknown)=>void;mocks.api.saveDraft.mockImplementation(()=>new Promise(resolve=>{finish=resolve}));render(<Editor/>);
 const button=await readyButton('Save draft');fireEvent.click(button);fireEvent.click(button);await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1));
 await act(async()=>{finish({...base,revision:'5'})});expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1);
});

it('replays full recipe bridge body and key across remount without resaving newer local params',async()=>{
 configureLayers();
 mocks.api.capabilities.mockResolvedValue({operations:[{operation:'render.compose',status:'ready',available:true,supported_backends:['cuda'],available_backends:['cuda']}]});
 mocks.api.saveDraft.mockImplementation(async(_asset,payload)=>({...payload,revision:'5'}));
 mocks.api.bridgeEditorRecipe.mockRejectedValue(new Error('offline after dispatch'));
 const view=render(<Editor/>);await screen.findByText(/revision 4/);
 const action=await screen.findByRole('button',{name:'Render full current recipe (review may be required)'});
 await waitFor(()=>expectEnabled(action));await clickWhenEnabled(action);
 await waitFor(()=>expect(mocks.api.bridgeEditorRecipe).toHaveBeenCalledTimes(1));
 await screen.findByRole('button',{name:'Retry saved full recipe render'});
 const first=mocks.api.bridgeEditorRecipe.mock.calls[0];view.unmount();
 mocks.api.capabilities.mockResolvedValue({operations:[]});
 render(<Editor/>);const retry=await screen.findByRole('button',{name:'Retry saved full recipe render'});
 await waitFor(()=>expectEnabled(retry));await clickWhenEnabled(retry);
 await waitFor(()=>expect(mocks.api.bridgeEditorRecipe).toHaveBeenCalledTimes(2));
 expect(mocks.api.bridgeEditorRecipe.mock.calls[1].slice(0,3)).toEqual(first.slice(0,3));expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1);
 expect(mocks.api.preview).not.toHaveBeenCalled();expect(mocks.api.freezeRenderDraft).not.toHaveBeenCalled();expect(mocks.api.editorCapabilities).not.toHaveBeenCalled();
});

it('retains a bridge attempt and does not freeze after its acknowledgement arrives after unmount',async()=>{
 const fixture=configureLayers();
 mocks.api.capabilities.mockResolvedValue({operations:[{operation:'render.compose',status:'ready',available:true,supported_backends:['cuda'],available_backends:['cuda']}]});
 mocks.api.saveDraft.mockImplementation(async(_asset,payload)=>({...payload,revision:'5'}));
 let finish!:(value:unknown)=>void;mocks.api.bridgeEditorRecipe.mockImplementation(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<Editor/>);await screen.findByText(/revision 4/);
 const action=screen.getByRole('button',{name:'Render full current recipe (review may be required)'});await waitFor(()=>expectEnabled(action));await clickWhenEnabled(action);
 await waitFor(()=>expect(mocks.api.bridgeEditorRecipe).toHaveBeenCalledTimes(1));view.unmount();
 await act(async()=>{finish(fixture.draft)});
 expect(mocks.api.freezeRenderDraft).not.toHaveBeenCalled();expect(mocks.api.preview).not.toHaveBeenCalled();
 expect([...mocks.storage.keys()].some(key=>key.startsWith('photo.editor-recipe-bridge-attempt.v1:'))).toBe(true);
});

async function seedAcceptedEffects(){
 const {EditorEffectsStore}=await import('./editor-effects-store');
 const {prepareEditorEffectsAttempt}=await import('./editor-effects-attempt');
 const value={...prepareEditorEffectsAttempt(layerId(20),{document_id:layerId(21),draft_document_id:layerId(22),draft_revision:'2',source:{asset_version_id:base.asset_version_id,source:{artifact_id:layerId(23),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},recipe:{artifact_id:layerId(24),sha256:'c'.repeat(64)}},layerId(25)),accepted:{run_id:layerId(26),status:'queued' as const}};
 const storage={getItem:async(key:string)=>mocks.storage.get(key)??null,setItem:async(key:string,value:string)=>{mocks.storage.set(key,value)},removeItem:async(key:string)=>{mocks.storage.delete(key)}};
 await new EditorEffectsStore(storage,mocks.auth.origin,'33333333-3333-4333-8333-333333333333',mocks.query.tenant,base.asset_version_id).write(value);return value;
}
it('restores accepted full recipe with status reads and never treats completed Apply as final',async()=>{
 const value=await seedAcceptedEffects();effectsAPI.editorEffectsStatus.mockResolvedValue({build_run_id:value.accepted.run_id,state:'sealing',build_status:'completed',seal_run_id:layerId(27),final_run_id:null,review_required:false});
 render(<Editor/>);await screen.findByText('Full recipe processing: sealing.');
 expect(effectsAPI.queueEditorEffects).not.toHaveBeenCalled();expect(mocks.api.runOutputs).not.toHaveBeenCalled();expect(mocks.api.freezeRenderDraft).not.toHaveBeenCalled();
});
it.each([401,403,404,410])('accepted full recipe denied status %s retains journal without another mutation',async status=>{
 await seedAcceptedEffects();effectsAPI.editorEffectsStatus.mockRejectedValue(new APIError(status,'FORBIDDEN'));
 render(<Editor/>);await waitFor(()=>expect(effectsAPI.editorEffectsStatus).toHaveBeenCalled());
 await screen.findByText('Editor access is no longer available. Reopen this photo after signing in or restoring access.');
 expect(effectsAPI.queueEditorEffects).not.toHaveBeenCalled();expect(mocks.api.runOutputs).not.toHaveBeenCalled();
 expect([...mocks.storage.keys()].some(key=>key.startsWith('photo.editor-effects.v1:'))).toBe(true);
});
it('clears effects-only final images and review navigation until fresh foreground authority returns',async()=>{
 const value=await seedAcceptedEffects(),final=layerId(28);
 const status={build_run_id:value.accepted.run_id,state:'composing',build_status:'completed',seal_run_id:layerId(27),final_run_id:final,review_required:true};
 effectsAPI.editorEffectsStatus.mockResolvedValueOnce(status);
 let resume!:(value:unknown)=>void;effectsAPI.editorEffectsStatus.mockImplementation(()=>new Promise(resolve=>{resume=resolve}));
 mocks.api.run.mockResolvedValue({id:final,kind:'v2',status:'waiting_review',revision:'2',counts:{total:1,succeeded:1,failed:0,skipped:0,review:1},stage:'review',progress:1,errors:[]});
 mocks.api.runOutputs.mockResolvedValue({items:[editorImage('https://images.example/full-recipe.jpg')]});
 render(<Editor/>);await screen.findByText('Open photographer review for full recipe');
 await waitFor(()=>expect(screen.queryAllByLabelText(en.render_image_accessibility).length).toBeGreaterThan(0));
 generationAppState('background');expect(screen.queryAllByLabelText(en.render_image_accessibility)).toHaveLength(0);expect(screen.queryByText('Open photographer review for full recipe')).toBeNull();
 generationAppState('active');await waitFor(()=>expect(effectsAPI.editorEffectsStatus).toHaveBeenCalledTimes(2));expect(screen.queryAllByLabelText(en.render_image_accessibility)).toHaveLength(0);expect(screen.queryByText('Open photographer review for full recipe')).toBeNull();
 await act(async()=>resume(status));await screen.findByText('Open photographer review for full recipe');expect(effectsAPI.queueEditorEffects).not.toHaveBeenCalled();
});
it('withdraws visible full recipe results after current final-output denial and retains the command',async()=>{
 const value=await seedAcceptedEffects(),final=layerId(28);
 effectsAPI.editorEffectsStatus.mockResolvedValue({build_run_id:value.accepted.run_id,state:'composing',build_status:'completed',seal_run_id:layerId(27),final_run_id:final,review_required:true});
 mocks.api.run.mockResolvedValue({id:final,kind:'v2',status:'waiting_review',revision:'2',counts:{total:1,succeeded:1,failed:0,skipped:0,review:1},stage:'review',progress:1,errors:[]});
 mocks.api.runOutputs.mockResolvedValueOnce({items:[editorImage('https://images.example/full-recipe.jpg')]}).mockRejectedValue(new APIError(403,'FORBIDDEN'));
 render(<Editor/>);await screen.findByText('Open photographer review for full recipe');generationAppState('background');generationAppState('active');
 await screen.findByText('Editor access is no longer available. Reopen this photo after signing in or restoring access.');expect(screen.queryAllByLabelText(en.render_image_accessibility)).toHaveLength(0);expect(screen.queryByText('Open photographer review for full recipe')).toBeNull();
 expect([...mocks.storage.keys()].some(key=>key.startsWith('photo.editor-effects.v1:'))).toBe(true);expect(effectsAPI.queueEditorEffects).not.toHaveBeenCalled();
});
it('ignores accepted full recipe status arriving after unmount',async()=>{
 const value=await seedAcceptedEffects();let finish!:(v:unknown)=>void;effectsAPI.editorEffectsStatus.mockImplementation(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<Editor/>);await waitFor(()=>expect(effectsAPI.editorEffectsStatus).toHaveBeenCalled());view.unmount();
 await act(async()=>finish({build_run_id:value.accepted.run_id,state:'composing',build_status:'completed',seal_run_id:layerId(27),final_run_id:layerId(28),review_required:false}));
 expect(mocks.api.run).not.toHaveBeenCalled();expect(mocks.api.runOutputs).not.toHaveBeenCalled();expect(effectsAPI.queueEditorEffects).not.toHaveBeenCalled();
});

 it.each([404,410])('revokes cached render presentation after parent status denial %s without losing recovery',async status=>{
 const ticket={run_id:layerId(30),state:'queued',child_run_ids:[layerId(31)]};
 const key='photo.editor-render.v1:'+JSON.stringify([mocks.auth.origin,'33333333-3333-4333-8333-333333333333',mocks.query.tenant,base.asset_version_id]);
 mocks.storage.set(key,JSON.stringify({schema_version:1,saved_at:Date.now(),ticket}));
 mocks.api.run.mockImplementation(async(id:string)=>{if(id===ticket.run_id)throw new APIError(status,'NOT_FOUND');return {id,kind:'v2',status:'completed',revision:'2',counts:{total:1,succeeded:1,failed:0,skipped:0,review:0},stage:'complete',progress:1,errors:[]}});
 mocks.api.runOutputs.mockResolvedValue({items:[editorImage('https://images.example/cached-render.jpg')]});
 render(<Editor/>);await screen.findByText('Editor access is no longer available. Reopen this photo after signing in or restoring access.');
 expect(screen.queryAllByLabelText(en.render_image_accessibility)).toHaveLength(0);expect(mocks.storage.has(key)).toBe(true);expect(effectsAPI.queueEditorEffects).not.toHaveBeenCalled();
 });

it('stops a hung pending-render forget and ignores its old finally during replacement save',async()=>{
 const {EditorRenderCache}=await import('./editor-render-cache');
 const {prepareEditorRenderSubmission}=await import('./editor-render-submit');
 const command=prepareEditorRenderSubmission({document_id:layerId(40),draft_document_id:layerId(41),draft_revision:'2',source:{asset_version_id:base.asset_version_id,source:{artifact_id:layerId(42),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},recipe:{artifact_id:layerId(43),sha256:'c'.repeat(64)}},'d'.repeat(64),'saved-snapshot','saved-render');
 const key='photo.editor-render.v1:'+JSON.stringify([mocks.auth.origin,'33333333-3333-4333-8333-333333333333',mocks.query.tenant,base.asset_version_id,'pending']);
 mocks.storage.set(key,JSON.stringify({schema_version:1,saved_at:Date.now(),project:layerId(44),command}));
 let finishForget!:(value:boolean)=>void,finishSave!:(value:unknown)=>void;
 const forget=vi.spyOn(EditorRenderCache.prototype,'clearPending').mockImplementation(()=>new Promise(resolve=>{finishForget=resolve}));
 mocks.api.saveDraft.mockImplementation(()=>new Promise(resolve=>{finishSave=resolve}));
 try{
  render(<Editor/>);await clickWhenEnabled(await screen.findByRole('button',{name:en.render_pending_forget}));
  await clickWhenEnabled(await screen.findByRole('button',{name:en.render_pending_forget_confirm}));
  await waitFor(()=>expect(forget).toHaveBeenCalledOnce());
  await clickWhenEnabled(screen.getByRole('button',{name:en.editor_stop_waiting}));
  expect(mocks.storage.has(key)).toBe(true);
  await clickSaveDraft();
  await waitFor(()=>expect(mocks.api.saveDraft).toHaveBeenCalledOnce());
  await act(async()=>{finishForget(true)});
  expect(screen.getByRole('button',{name:'Save draft'}).getAttribute('aria-disabled')).toBe('true');
  expect(screen.getByRole('button',{name:en.render_pending_retry})).toBeTruthy();
  await act(async()=>{finishSave({...base,revision:'5'})});
 }finally{forget.mockRestore();}
});
function acknowledgeNewBridge(){
 const fixture=configureLayers();mocks.api.saveDraft.mockImplementation(async(_asset,payload)=>({...payload,revision:'5'}));
 mocks.api.bridgeEditorRecipe.mockImplementation(async(_asset,body)=>{const recipe={...body.recipe,layer_stack:body.layers===null?null:{artifact_id:layerId(75),sha256:layerDigest(body.layers)},overrides:[...body.recipe.overrides,{artifact_id:layerId(71),sha256:'b'.repeat(64)}]};mocks.api.v2Document.mockResolvedValue(recipe);return{...fixture.draft,revision:'3',document:{artifact_id:layerId(72),sha256:layerDigest(recipe)},data:{...fixture.draft.data,project_id:layerId(73),revision:'3',current_document_id:layerId(74)}};});
 mocks.api.capabilities.mockResolvedValue({operations:[{operation:'render.compose',status:'ready',available:true,supported_backends:['mps'],available_backends:['mps']}]});
}
it('blocks a new exact acknowledged full recipe before freeze when combined CPU stages are unavailable',async()=>{
 acknowledgeNewBridge();mocks.api.editorCapabilities.mockResolvedValue({available:false,reason:'EDITOR_SEAL_CAPABILITY_UNAVAILABLE'});render(<Editor/>);const action=await screen.findByRole('button',{name:'Render full current recipe (review may be required)'});await waitFor(()=>expectEnabled(action));await clickWhenEnabled(action);
 await waitFor(()=>expect(mocks.api.editorCapabilities).toHaveBeenCalledOnce());expect(mocks.api.freezeRenderDraft).not.toHaveBeenCalled();await screen.findByText(en.capability_unsupported);expect([...mocks.storage.keys()].some(k=>k.startsWith('photo.editor-recipe-bridge-attempt.v1:'))).toBe(true);
});
it('ignores late combined capability success after leaving the editor and retains exact bridge recovery',async()=>{
 acknowledgeNewBridge();let finish!:(v:unknown)=>void;mocks.api.editorCapabilities.mockImplementation(()=>new Promise(done=>{finish=done}));const view=render(<Editor/>);const action=await screen.findByRole('button',{name:'Render full current recipe (review may be required)'});await waitFor(()=>expectEnabled(action));await clickWhenEnabled(action);await waitFor(()=>expect(mocks.api.editorCapabilities).toHaveBeenCalledOnce());view.unmount();await act(async()=>finish({available:true}));expect(mocks.api.freezeRenderDraft).not.toHaveBeenCalled();expect([...mocks.storage.keys()].some(k=>k.startsWith('photo.editor-recipe-bridge-attempt.v1:'))).toBe(true);
});
it('does not require a new live worker advisory to restore accepted effects',async()=>{const saved=await seedAcceptedEffects();effectsAPI.editorEffectsStatus.mockResolvedValue({build_run_id:saved.accepted.run_id,state:'sealing',build_status:'completed',seal_run_id:layerId(27),final_run_id:null,review_required:false});render(<Editor/>);await screen.findByText('Full recipe processing: sealing.');expect(mocks.api.editorCapabilities).not.toHaveBeenCalled();});
it('keeps accepted effects recovery clickable after render.compose disappears',async()=>{const saved=await seedAcceptedEffects();mocks.api.capabilities.mockResolvedValue({operations:[]});effectsAPI.editorEffectsStatus.mockResolvedValue({build_run_id:saved.accepted.run_id,state:'sealing',build_status:'completed',seal_run_id:layerId(27),final_run_id:null,review_required:false});render(<Editor/>);const recover=await screen.findByRole('button',{name:'Recover saved full recipe render'});await waitFor(()=>expectEnabled(recover));await clickWhenEnabled(recover);await waitFor(()=>expect(effectsAPI.editorEffectsStatus.mock.calls.length).toBeGreaterThanOrEqual(2));expect(effectsAPI.queueEditorEffects).not.toHaveBeenCalled();expect(mocks.api.editorCapabilities).not.toHaveBeenCalled();expect(mocks.api.freezeRenderDraft).not.toHaveBeenCalled();});

it('mounted Save edits reuses its exact durable body/key after unknown response without saving another draft or override',async()=>{
 mocks.query.collection='55555555-5555-4555-8555-555555555555';mocks.query.item='66666666-6666-4666-8666-666666666666';
 mocks.api.collection.mockResolvedValue({id:mocks.query.collection,project_id:'77777777-7777-4777-8777-777777777777',revision:'1',name:'Source'});mocks.api.styles.mockResolvedValue({items:[]});
 mocks.api.saveDraft.mockImplementation(async(_asset,payload)=>({...payload,revision:'5'}));mocks.api.createOverride.mockResolvedValue({id:'88888888-8888-4888-8888-888888888888'});
 const ticket={run_id:'99999999-9999-4999-8999-999999999999',output_collection_id:'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',status:'queued'};
 mocks.api.submitSaveEdits.mockRejectedValueOnce(Error('lost reply')).mockResolvedValue(ticket);
 render(<Editor/>);const save=await screen.findByRole('button',{name:'Save edits as new collection'});await waitFor(()=>expectEnabled(save));await clickWhenEnabled(save);
 await waitFor(()=>expect(mocks.api.submitSaveEdits).toHaveBeenCalledTimes(1));await screen.findByText('The saved request has been kept. Retry to recover the same collection without creating another request.');
 await clickWhenEnabled(screen.getByRole('button',{name:'Save edits as new collection'}));await waitFor(()=>expect(mocks.api.submitSaveEdits).toHaveBeenCalledTimes(2));
 expect(mocks.api.submitSaveEdits.mock.calls[1].slice(0,3)).toEqual(mocks.api.submitSaveEdits.mock.calls[0].slice(0,3));expect(mocks.api.saveDraft).toHaveBeenCalledTimes(1);expect(mocks.api.createOverride).toHaveBeenCalledTimes(1);
 await screen.findByRole('button',{name:'Start another saved collection'});await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Save edits as new collection'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Save edits as new collection'}));await waitFor(()=>expect(mocks.api.me.mock.calls.length).toBeGreaterThanOrEqual(4));expect(mocks.api.submitSaveEdits).toHaveBeenCalledTimes(2);
});

it('retained old Save edits acknowledgement cannot clear a newer accepted collection request',async()=>{
 mocks.query.collection='55555555-5555-4555-8555-555555555555';mocks.query.item='66666666-6666-4666-8666-666666666666';
 mocks.api.collection.mockResolvedValue({id:mocks.query.collection,project_id:'77777777-7777-4777-8777-777777777777',revision:'1',name:'Source'});mocks.api.styles.mockResolvedValue({items:[]});
 mocks.api.saveDraft.mockImplementation(async(_asset,payload)=>({...payload,revision:String(Number(payload.revision)+1)}));mocks.api.createOverride.mockResolvedValue({id:'88888888-8888-4888-8888-888888888888'});
 const first={run_id:'99999999-9999-4999-8999-999999999999',output_collection_id:'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',status:'queued'},second={...first,run_id:'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'};mocks.api.submitSaveEdits.mockResolvedValueOnce(first).mockResolvedValueOnce(second);
 render(<Editor/>);await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Save edits as new collection'})));await clickWhenEnabled(screen.getByRole('button',{name:'Save edits as new collection'}));
 await screen.findByRole('button',{name:'Start another saved collection'});const old=retainedSaveEdits.acknowledgements[0];expect(old).toBeTypeOf('function');await clickWhenEnabled(screen.getByRole('button',{name:'Start another saved collection'}));await waitFor(()=>expect(screen.queryByRole('button',{name:'Start another saved collection'})).toBeNull());
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Save edits as new collection'})));await clickWhenEnabled(screen.getByRole('button',{name:'Save edits as new collection'}));await waitFor(()=>expect(mocks.api.submitSaveEdits).toHaveBeenCalledTimes(2));await screen.findByRole('button',{name:'Start another saved collection'});
 const before=mocks.api.me.mock.calls.length;await act(async()=>old());expect(mocks.api.me).toHaveBeenCalledTimes(before);expect([...mocks.storage.values()].some(raw=>raw.includes(second.run_id))).toBe(true);expect(screen.getByRole('button',{name:'Start another saved collection'})).toBeTruthy();
});
