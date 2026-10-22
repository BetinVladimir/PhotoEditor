// @vitest-environment jsdom
import {cleanup,render,screen} from '@testing-library/react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
const state=vi.hoisted(()=>({locale:'en' as 'en'|'ru'|'bg',settings:vi.fn(),capabilities:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({tenant:'11111111-1111-4111-8111-111111111111'})}));
vi.mock('./auth-context',()=>{const auth={origin:'https://quotas.test',client:()=>({})};return {useAuth:()=>auth}});
vi.mock('./owner-api',()=>({OwnerAPI:class {tenantSettings=state.settings;capabilities=state.capabilities;}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:state.locale,t:(key:keyof typeof en)=>({en,ru,bg})[state.locale][key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
import Quotas from '../screens/settings/quotas';
beforeEach(()=>{vi.resetAllMocks();state.locale='en';state.settings.mockResolvedValue({quotas:{storage_bytes:1073741824,source_files:12345,max_active_gpu_tasks:2}});state.capabilities.mockResolvedValue({operations:[{operation:'denoise',available:true,reason:null},{operation:'relight',available:false,reason:'no_qualified_worker'}]});});
afterEach(cleanup);
it('changes all quota labels and number formatting without losing real capability availability',async()=>{
 const view=render(<Quotas/>);await screen.findByText('denoise');
 for(const [locale,catalog] of [['en',en],['ru',ru],['bg',bg]] as const){
  state.locale=locale;view.rerender(<Quotas/>);
  for(const key of ['title','storage','source_files','gpu_tasks','operations','available'] as const)expect(screen.getByText(catalog[`settings_quotas_${key}`])).toBeTruthy();
  expect(screen.getByText((_text,element)=>element?.children.length===0&&element.textContent===(12345).toLocaleString(locale))).toBeTruthy();
  expect(screen.getByText(`${catalog.settings_quotas_unavailable}: no_qualified_worker`)).toBeTruthy();
  expect(screen.getByText('relight')).toBeTruthy();expect(screen.getByText(`${(1).toLocaleString(locale,{minimumFractionDigits:1,maximumFractionDigits:1})} ${catalog.ui_66a1561b84b4}`)).toBeTruthy();
 }
 expect(state.settings).toHaveBeenCalledOnce();expect(state.capabilities).toHaveBeenCalledOnce();
});
it.each(['ru','bg'] as const)('translates empty operations in %s without claiming availability',async locale=>{
 state.locale=locale;state.capabilities.mockResolvedValue({operations:[]});render(<Quotas/>);
 const catalog={en,ru,bg}[locale];await screen.findByText(catalog.settings_quotas_empty);
 expect(screen.queryByText(catalog.settings_quotas_available)).toBeNull();
});
