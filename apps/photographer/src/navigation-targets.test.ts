import {expect,it} from 'vitest';
import {profileTarget,workspaceSettingsTargets,sessionAnalysisTarget} from './navigation-targets';
import {workspaceScope} from './workspace-access';
const tenant='11111111-1111-4111-8111-111111111111',id='22222222-2222-4222-8222-222222222222',project='33333333-3333-4333-8333-333333333333';
it('preserves exact tenant in every settings destination while profile remains global',()=>{
 expect(profileTarget).toEqual({pathname:'/settings/profile'});
 for(const item of workspaceSettingsTargets(tenant)){expect(item.href.params).toEqual({tenant});expect(workspaceScope(item.href.pathname,item.href.params)?.tenant).toBe(tenant);}
 expect(()=>workspaceSettingsTargets('invalid')).toThrow();
});
it('uses the actual capture session id and project for Viewer-readable analysis',()=>{
 const target=sessionAnalysisTarget(tenant,id,project);expect(target.params).toEqual({tenant,id,project});expect(workspaceScope(target.pathname,target.params)).toEqual({tenant,project});
 expect(()=>sessionAnalysisTarget(tenant,id,'')).toThrow();
});
