// @vitest-environment jsdom
import {afterEach,it,expect,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
const locale=vi.hoisted(()=>({value:'en' as 'en'|'ru'|'bg'}));
const catalogs={en,ru,bg};
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>catalogs[locale.value][key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {SemanticReviewCloseControl} from './semantic-review-close';
const id='11111111-1111-4111-8111-111111111111';
const status={run_id:id,workflow_id:id,revision:'9',state:'waiting_review' as const,plan_sha:'a'.repeat(64),remaining:0,can_close:true};
afterEach(()=>{cleanup();locale.value='en';});
it.each(['ru','bg'] as const)('keeps immutable close attempt while translating its retry/error to %s',async language=>{const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),closeSemanticReview:vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce({run_id:id,workflow_id:id,revision:'10',state:'completed',outputs:[],error_code:null})};const element=<SemanticReviewCloseControl client={client} tenant={id} intent={id}/>;const view=render(element);const close=await screen.findByRole('button',{name:en.semantic_close_action});await waitFor(()=>expect(close.hasAttribute('disabled')).toBe(false));fireEvent.click(close);const retry=await screen.findByRole('button',{name:en.semantic_close_retry});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));const original=structuredClone(client.closeSemanticReview.mock.calls[0].slice(0,4));locale.value=language;view.rerender(<SemanticReviewCloseControl client={client} tenant={id} intent={id}/>);expect(screen.getByRole('alert').textContent).toBe(catalogs[language].semantic_close_unknown);fireEvent.click(screen.getByRole('button',{name:catalogs[language].semantic_close_retry}));await screen.findByText(catalogs[language].semantic_close_completed);expect(client.closeSemanticReview.mock.calls[1].slice(0,4)).toEqual(original);expect(client.semanticReviewStatus).toHaveBeenCalledTimes(1);});
