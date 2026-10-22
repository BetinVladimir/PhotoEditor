// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import en from '../../../locales/en/common.json';
const calls=vi.hoisted(()=>({request:vi.fn(),tokens:vi.fn(),persist:vi.fn(),replace:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({router:{replace:calls.replace}}));
const client={request:calls.request},auth={origin:'https://login.example',clientType:'web',client:()=>client,principal:()=>null,sessionGeneration:()=>0,setTokens:calls.tokens,persist:calls.persist,acceptLoginTokens:async(value:unknown,signal?:AbortSignal)=>{calls.tokens(value);await Promise.race([calls.persist(),new Promise<never>((_resolve,reject)=>signal?.addEventListener('abort',()=>reject(new DOMException('cancelled','AbortError')),{once:true}))]);signal?.throwIfAborted();return 0;}};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:'en',t:(key:string)=>(en as Record<string,string>)[key]??key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import Login from '../screens/login';
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done;});return{promise,resolve};}
afterEach(()=>{cleanup();vi.useRealTimers();vi.resetAllMocks();});
it('ignores cancelled late verification and permits explicit new code request',async()=>{
 const late=deferred<unknown>();calls.request.mockResolvedValueOnce({csrf_token:'bootstrap'}).mockResolvedValueOnce({challenge_id:'challenge'}).mockReturnValueOnce(late.promise);
 render(<Login/>);fireEvent.change(screen.getByLabelText('Email'),{target:{value:'person@example.test'}});fireEvent.click(screen.getByRole('button',{name:'Continue'}));await act(async()=>{});
 fireEvent.change(screen.getByLabelText('One-time code'),{target:{value:'123456'}});fireEvent.click(screen.getByRole('button',{name:'Sign in'}));await act(async()=>{});fireEvent.click(screen.getByRole('button',{name:'Stop loading'}));
 await act(async()=>{late.resolve({access_token:'late',csrf_token:'late'});});expect(calls.tokens).not.toHaveBeenCalled();expect(calls.persist).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:en.login_request_new}));expect(screen.getByLabelText('Email')).toBeTruthy();expect(calls.request.mock.calls.filter(call=>String(call[0]).includes('/otp/'))).toHaveLength(2);
});
it('bounds noncooperative OTP request and rejects its late challenge',async()=>{
 vi.useFakeTimers();const late=deferred<unknown>();calls.request.mockResolvedValueOnce({csrf_token:'bootstrap'}).mockReturnValueOnce(late.promise);render(<Login/>);
 fireEvent.change(screen.getByLabelText('Email'),{target:{value:'person@example.test'}});fireEvent.click(screen.getByRole('button',{name:'Continue'}));await act(async()=>{});
 act(()=>vi.advanceTimersByTime(15000));expect(screen.getByRole('alert').textContent).toBe(en.read_error);
 await act(async()=>{late.resolve({challenge_id:'late'});});expect(screen.queryByLabelText('One-time code')).toBeNull();
});
it('does not persist authentication returned after route unmount',async()=>{
 const late=deferred<unknown>();calls.request.mockResolvedValueOnce({csrf_token:'bootstrap'}).mockResolvedValueOnce({challenge_id:'challenge'}).mockReturnValueOnce(late.promise);
 const view=render(<Login/>);fireEvent.change(screen.getByLabelText('Email'),{target:{value:'person@example.test'}});fireEvent.click(screen.getByRole('button',{name:'Continue'}));await act(async()=>{});
 fireEvent.change(screen.getByLabelText('One-time code'),{target:{value:'123456'}});fireEvent.click(screen.getByRole('button',{name:'Sign in'}));view.unmount();
 await act(async()=>{late.resolve({access_token:'late',csrf_token:'late'});});expect(calls.tokens).not.toHaveBeenCalled();expect(calls.replace).not.toHaveBeenCalled();
});
it('repeats the exact verification key, code and bootstrap after an uncertain response',async()=>{
 calls.request.mockResolvedValueOnce({csrf_token:'bootstrap'}).mockResolvedValueOnce({challenge_id:'challenge'}).mockRejectedValueOnce(new Error('lost response')).mockResolvedValueOnce({access_token:'accepted',csrf_token:'session'});
 render(<Login/>);fireEvent.change(screen.getByLabelText('Email'),{target:{value:'person@example.test'}});fireEvent.click(screen.getByRole('button',{name:'Continue'}));await act(async()=>{});
 fireEvent.change(screen.getByLabelText('One-time code'),{target:{value:'123456'}});fireEvent.click(screen.getByRole('button',{name:'Sign in'}));await act(async()=>{});
 fireEvent.click(screen.getByRole('button',{name:'Sign in'}));await act(async()=>{});
 const verification=calls.request.mock.calls.filter(call=>String(call[0]).endsWith('/otp/verify')).map(call=>call[1]);
 expect(verification).toHaveLength(2);expect(verification[1].idempotencyKey).toBe(verification[0].idempotencyKey);expect(verification[1].body).toEqual(verification[0].body);expect(verification[1].headers).toEqual({'X-CSRF-Token':'bootstrap'});expect(calls.tokens).toHaveBeenCalledTimes(1);
});

it.each(['cancel','timeout'])('keeps a pending credential acceptance bounded by %s and never navigates after late persistence',async(mode)=>{
 vi.useFakeTimers();const late=deferred<void>();calls.persist.mockReturnValue(late.promise);
 calls.request.mockResolvedValueOnce({csrf_token:'bootstrap'}).mockResolvedValueOnce({challenge_id:'challenge'}).mockResolvedValueOnce({access_token:'accepted',csrf_token:'session'});
 render(<Login/>);fireEvent.change(screen.getByLabelText('Email'),{target:{value:'person@example.test'}});fireEvent.click(screen.getByRole('button',{name:'Continue'}));await act(async()=>{});
 fireEvent.change(screen.getByLabelText('One-time code'),{target:{value:'123456'}});fireEvent.click(screen.getByRole('button',{name:'Sign in'}));await act(async()=>{});expect(calls.persist).toHaveBeenCalledOnce();
 if(mode==='cancel')fireEvent.click(screen.getByRole('button',{name:'Stop loading'}));else act(()=>vi.advanceTimersByTime(15000));
 await act(async()=>{});expect(screen.getByRole('button',{name:'Sign in'})).not.toHaveProperty('disabled',true);expect(calls.replace).not.toHaveBeenCalled();
 await act(async()=>late.resolve());expect(calls.replace).not.toHaveBeenCalled();
});
