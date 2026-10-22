import {useSyncExternalStore} from 'react';
import {afterEach} from 'vitest';
import {localizedURL,splitLocaleURL} from '../locale-url';
/** URL-backed navigation boundary for standalone screen DOM fixtures.
 * The real LocaleProvider remains mounted; changing locale updates the URL
 * without remounting the screen or replacing its command/recovery state.
 */
const listeners=new Set<()=>void>();
export function setFixtureLocale(locale:'en'|'ru'|'bg'){
 window.history.replaceState(null,'',localizedURL(window.location.pathname+window.location.search+window.location.hash,locale));
 for(const listener of listeners)listener();
}
const navigation={current:{getRootState:()=>({key:'fixture-stack',index:0,routes:[{name:'[locale]',key:'fixture-locale',state:{key:'fixture-child-stack',index:0,routes:[{name:'projects',key:'fixture-screen',params:{locale:'en'}}]}}]}),dispatch:(action:{source:string;target:string;payload:{params:{locale:'en'|'ru'|'bg'}}})=>{if(action.source!=='fixture-screen'||action.target!=='fixture-child-stack')throw new Error('LOCALE_ACTION_MUST_TARGET_ACTIVE_SCREEN');setFixtureLocale(action.payload.params.locale)}}};
function replace(href:string){window.history.replaceState(null,'',href);for(const listener of listeners)listener();}
if(typeof window!=='undefined'&&!splitLocaleURL(window.location.pathname).locale)setFixtureLocale('en');
afterEach(()=>{if(typeof window!=='undefined')window.history.replaceState(null,'','/en');});
export const localeRouterFixture={
 usePathname:()=>useSyncExternalStore(listener=>{listeners.add(listener);return()=>{listeners.delete(listener)}},()=>window.location.pathname,()=>'/en'),
 useNavigationContainerRef:()=>navigation,
 router:{replace},
};
