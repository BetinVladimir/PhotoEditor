import {expect,it} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
import {localizeUIMessage,localizeUIValue,formatUIString} from './ui-localization';
const translate=(key:string,params?:Record<string,string|number>)=>{
 if(!Object.hasOwn(en,key))throw new Error('Unexpected catalog key');
 return formatUIString((en as Record<string,string>)[key],params);
};
it.each(['__proto__','constructor','toString','hasOwnProperty'])('unknown special name %s cannot become a catalog key',value=>{
 expect(localizeUIValue(value,translate)).toBe(value);
 expect(localizeUIMessage(value,translate,'Safe localized error',true)).toBe('Safe localized error');
 expect(localizeUIMessage(value,translate,'Safe localized error',false)).toBe(value);
});
it('known messages and enum display labels still use actual catalogs',()=>{
 expect(localizeUIValue('queued',translate)).toBe('Queued');
 expect(localizeUIMessage('Unable to load run',translate,'Fallback',true)).toBe('Unable to load run');
 expect(localizeUIMessage('Download job 123 is queued.',translate,'Fallback')).toBe('Download job 123 is Queued.');
});

// Existing state-machine message keys must resolve to their established translations,
// never to a generated translation of the technical key itself.
const stateMessageKeys=[
  "capture_access_denied",
  "capture_draft_storage_error",
  "capture_scope",
  "capture_load_error",
  "capture_conflict",
  "capture_label_error",
  "capture_save_error",
  "capture_more_sessions_error",
  "multi_scope_error",
  "multi_recovery_only",
  "multi_unavailable",
  "multi_load_error",
  "multi_frame_error",
  "multi_edit_error",
  "multi_unknown",
  "multi_clear_error",
  "render_results_retrying",
  "render_results_access_lost",
  "scene_access_denied",
  "scene_reference_failed",
  "session_load_failed",
  "scene_reference_conflict",
  "scene_reference_unavailable",
  "scene_apply_failed",
  "scene_load_failed",
  "scene_apply_conflict",
  "settings_members_restore_failed",
  "settings_members_uncertain",
  "settings_members_rejected",
  "settings_members_confirmed",
  "settings_members_recovery_stopped",
  "retention_invalid",
  "workspace_restore_failed",
  "workspace_name_too_long",
  "workspace_creation_uncertain",
  "workspace_recovery_cancelled",
  "capture_collections_error",
  "capture_template_unavailable",
  "capability_unsupported",
  "capture_create_error",
  "interchange_review_unavailable",
  "interchange_recovery_unresolved",
  "interchange_error_review_changed_selection",
  "interchange_error_review_unavailable",
  "interchange_error_review_changed",
  "interchange_error_decisions",
  "interchange_error_archive_unavailable",
  "interchange_error_download",
  "interchange_error_close",
  "interchange_error_job_unavailable",
  "interchange_error_job_load",
  "interchange_mask_decision_error",
  "interchange_mask_entries_error",
  "interchange_mask_candidates_error",
  "interchange_mask_preview_error",
  "semantic_progress_failed",
  "semantic_create_invalid",
  "semantic_create_unknown",
  "semantic_create_rejected",
  "semantic_preview_unavailable",
  "semantic_preview_expired",
  "semantic_preview_failed",
  "semantic_preview_display_failed",
  "semantic_close_load_failed",
  "semantic_close_rejected",
  "semantic_close_unknown",
  "semantic_review_limit",
  "semantic_review_load_failed",
  "semantic_review_unknown",
  "semantic_review_rejected",
  "semantic_review_read_failed",
  "semantic_results_failed"
] as const;
it.each(stateMessageKeys)('retains established translations for state message %s',key=>{
 for(const catalog of [en,ru,bg]){
  const t=(name:string,params?:Record<string,string|number>)=>formatUIString((catalog as Record<string,string>)[name],params);
  expect(localizeUIMessage(key,t,catalog.read_error,true,[en,ru,bg])).toBe(catalog[key]);
 }
});

it('localizes the completed-with-errors bulk status in every supported locale',()=>{
 for(const catalog of [en,ru,bg])expect(localizeUIValue('completed_with_errors',key=>(catalog as Record<string,string>)[key])).toBe(catalog.render_state_completed_with_errors);
});
