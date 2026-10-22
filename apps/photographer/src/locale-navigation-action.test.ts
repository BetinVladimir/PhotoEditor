import {expect,it} from 'vitest';
import {localeParamAction} from './locale-url';
it('updates the active child locale rather than an inherited stale parent param',()=>{
 const state={key:'root',index:0,routes:[{name:'[locale]',key:'localized',params:{locale:'en'},state:{key:'inner',index:1,routes:[{name:'login',key:'old'},{name:'editor/[version]',key:'editor',params:{locale:'en',version:'version-one',tenant:'tenant-one'}}]}}]};
 const before=JSON.stringify(state);
 expect(localeParamAction(state,'bg')).toEqual({type:'SET_PARAMS',source:'editor',target:'inner',payload:{params:{locale:'bg'}}});
 expect(JSON.stringify(state)).toBe(before);
});
it('does not grant a locale action outside the localized branch',()=>{
 expect(()=>localeParamAction({key:'root',routes:[{name:'login',key:'login'}]},'ru')).toThrow('ACTIVE_LOCALE_ROUTE_MISSING');
});
