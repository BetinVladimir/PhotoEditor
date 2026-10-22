// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {clickWhenEnabled} from './test/ui-readiness';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
const media=vi.hoisted(()=>({handlers:[] as Array<{onError:()=>void;source:{uri:string}}>}));
vi.mock('react-native',async()=>{const native=await import('react-native-web'),React=await import('react');return {...native,Image:(props:{onError:()=>void;source:{uri:string};accessibilityLabel:string})=>{media.handlers.push(props);return React.createElement('img',{src:props.source.uri,'aria-label':props.accessibilityLabel,onError:props.onError});}};});
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {SemanticPreview} from './semantic-preview';
const id='11111111-1111-4111-8111-111111111111';
afterEach(()=>{cleanup();vi.restoreAllMocks();media.handlers=[];});
it('retained old image error and expiry cannot clear a replacement preview',async()=>{
 const timeout=globalThis.setTimeout,expiries:Array<()=>void>=[];
 vi.spyOn(globalThis,'setTimeout').mockImplementation(((handler:TimerHandler,delay?:number,...args:unknown[])=>{if(typeof handler==='function'&&delay!==undefined&&delay>50000)expiries.push(handler as ()=>void);return timeout(handler,delay,...args);}) as typeof setTimeout);
 const image=(url:string)=>({artifact_id:id,sha256:'a'.repeat(64),media_type:'image/png' as const,width:100,height:100,url,expires_at:new Date(Date.now()+60000).toISOString()});
 const response=(url:string)=>({schema_version:1 as const,tenant_id:id,target_id:id,child_run_id:id,working_image:{artifact_id:id,sha256:'b'.repeat(64)},after:image(url)});
 const semanticAppliedPreview=vi.fn().mockResolvedValueOnce(response('https://objects.test/old')).mockResolvedValueOnce(response('https://objects.test/new'));
 render(<SemanticPreview client={{semanticAppliedPreview}} tenant={id} target={id} run={id}/>);fireEvent.click(screen.getByText('semantic_preview_view'));await screen.findByLabelText('semantic_preview_image');
 const old=media.handlers.find(value=>value.source.uri==='https://objects.test/old')!,oldExpiry=expiries[0];expect(oldExpiry).toBeTypeOf('function');
 await clickWhenEnabled(screen.getByRole('button',{name:'semantic_preview_reload'}));await waitFor(()=>expect(screen.getByLabelText('semantic_preview_image').getAttribute('src')).toBe('https://objects.test/new'));
 act(()=>{old.onError();oldExpiry();});expect(screen.getByLabelText('semantic_preview_image').getAttribute('src')).toBe('https://objects.test/new');expect(screen.queryByRole('alert')).toBeNull();
});
