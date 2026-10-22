import {Button} from 'react-native-paper';
import { ActivityIndicator, Text, TouchableRipple as Pressable } from 'react-native-paper';
import { clearEditorSaveEditsAttempts } from './editor-save-edits-attempt';
import { clearAccessLinkAttempts } from './access-link-attempt-store';
import { clearSectionCreateAttempts } from './section-create-attempt-store';
import { clearCollectionBulkAttempts } from './collection-bulk-store';
import { clearInvitationAttempts } from './invitation-attempt-store';
import { clearCaptureCreateAttempts } from './capture-create-attempt-store';
import { clearCatalogCreateAttempts } from './catalog-create-attempt-store';
import { clearWorkspaceCreateAttempts } from './workspace-create-attempt-store';
import { clearPresetMetadataAttempts } from './preset-metadata-attempt-store';
import { clearTrainingAttempts } from "./training-attempt-store";
import { useReadResource } from "./use-read-resource";
import { ReadResourceState } from "./read-resource-state";
import { useLocale } from "./locale-context";
import { clearLensAttempts } from './lens-journal-lifecycle';
import { clearExportBatchAttempts } from './export-batch-attempt-store';
import { clearPresetCreateAttempts } from './preset-create-attempt-store';
import { clearDownloadJobs } from "./download-job-store";
import { clearReviewDecisionAttempts } from './review-decision-attempt-store';
import { clearCollectionCreateAttempts } from './collection-create-attempt-store';
import { clearContactSheetAttempts } from './contact-sheet-attempt-store';
import { clearCollectionVersionAttempts } from './collection-version-store';
import { clearRelightReviewDecisionAttempts } from './relight-review-decision-attempt-store';
import { clearEditorGenerationAttempts } from './editor-generation-store';
import { clearImportMaskAttempts } from './import-mask-attempt-store';
import { clearSemanticApplyAttempts } from './semantic-apply-attempt-store';
import { clearCollectionApplyAttempts } from './collection-apply-store';
import { clearRunRetryAttempts } from './run-retry-attempt-store';
import { clearRunCancelAttempts } from './run-cancel-attempt-store';
import { clearUploadStorage } from './upload-storage';
import { clearInterchangeArchiveAttempts } from './interchange-archive-attempt-store';
import { clearInterchangeSnapshotAttempts } from './interchange-snapshot-attempt-store';
import { clearInterchangeCreateAttempts } from './interchange-create-attempt-store';
import { clearInterchangeReviewAttempts } from './interchange-review-attempt-store';
import { clearPreparedReviewAttempts } from './prepared-review-attempt-store';
import { clearDeliveryPublishAttempts } from './delivery-publish-attempt-store';
import { clearSigningAttempts } from './signing-attempt-store';
import { clearDeliveryCreateAttempts } from "./delivery-create-attempt-store";
import { clearMultiApplyAttempts } from "./multi-apply-store";
import { clearMultiAttempts } from './multi-attempt-store';
import { clearPerspectiveApplyAttempts } from "./perspective-apply-store";
import { clearPerspectiveLinesAttempts } from './perspective-lines-attempt-store';
import { clearNegativeAttempts } from "./negative-attempt-store";
import { clearPerspectiveAttempts } from "./perspective-attempt-store";
import { clearRelightAnalysisAttempts } from "./relight-analysis-attempt-store";
import { clearRelightAttempts } from "./relight-attempt-store";
import { clearGlareDonorAttempts as clearGlareDonorJournal } from "./glare-donor-attempt-store";
import { clearGlareSourceAttempts } from "./glare-source-attempt";
const clearGlareDonorAttempts = (storage: Parameters<typeof clearGlareDonorJournal>[0], origin: string) => Promise.all([clearGlareDonorJournal(storage, origin), clearGlareSourceAttempts(storage, origin)]);
import { clearOpenEyesAttempts } from "./open-eyes-attempt-store";
import { clearReflectionAttempts } from "./reflection-attempt-store";
import { clearLocalPortraitAttempts } from "./local-portrait-attempt-store";
import { clearSemanticAttempts } from "./semantic-attempt-store";
import { clearSceneApplyAttempts } from './scene-apply-attempt-store';
import { clearSceneReferenceAttempts } from './scene-reference-attempt-store';
import { clearSessionAnalysisAttempts } from './session-analysis-attempt-store';
import { clearSessionPolicyDrafts } from './session-policy-draft';
import { clearCullingDecisionAttempts } from './culling-decision-attempt-store';
import { clearEditorRecipeBridges } from './editor-recipe-bridge-store';
import { clearEditorEffectsAttempts } from './editor-effects-store';
import { clearEditorRenderCaches } from './editor-render-cache';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { clearCaptureDrafts } from './capture-draft-store';
import { clearEditorDraftCaches } from './editor-draft-cache';
import { createContext, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { OwnerAuth } from "./auth";
import { Platform } from "react-native";
import { View } from "react-native";
const AuthContext = createContext<OwnerAuth | null>(null);
export function AuthProvider({ children }: {
    children: ReactNode;
}) {
    const auth = useMemo(() => new OwnerAuth(process.env.EXPO_PUBLIC_API_ORIGIN || "", Platform.OS === "web" ? "web" : "native", fetch, async () => { const origin = process.env.EXPO_PUBLIC_API_ORIGIN || ""; await Promise.all([clearEditorSaveEditsAttempts(AsyncStorage, origin), clearAccessLinkAttempts(AsyncStorage, origin), clearInvitationAttempts(AsyncStorage, origin), clearCaptureCreateAttempts(AsyncStorage, origin), clearCatalogCreateAttempts(AsyncStorage, origin), clearCollectionBulkAttempts(AsyncStorage, origin), clearSectionCreateAttempts(AsyncStorage, origin), clearWorkspaceCreateAttempts(AsyncStorage, origin), clearPresetMetadataAttempts(AsyncStorage, origin), clearTrainingAttempts(AsyncStorage, origin), clearPresetCreateAttempts(AsyncStorage, origin), clearExportBatchAttempts(AsyncStorage, origin), clearDownloadJobs(AsyncStorage, origin), clearReviewDecisionAttempts(AsyncStorage, origin), clearCollectionVersionAttempts(AsyncStorage, origin), clearEditorGenerationAttempts(AsyncStorage, origin), clearImportMaskAttempts(AsyncStorage, origin), clearRunCancelAttempts(AsyncStorage, origin), clearRunRetryAttempts(AsyncStorage, origin), clearCollectionApplyAttempts(AsyncStorage, origin), clearUploadStorage(AsyncStorage, origin), clearInterchangeArchiveAttempts(AsyncStorage, origin), clearInterchangeSnapshotAttempts(AsyncStorage, origin), clearInterchangeCreateAttempts(AsyncStorage, origin), clearInterchangeReviewAttempts(AsyncStorage, origin), clearSigningAttempts(AsyncStorage, origin), clearDeliveryCreateAttempts(AsyncStorage, origin), clearDeliveryPublishAttempts(AsyncStorage, origin), clearPreparedReviewAttempts(AsyncStorage, origin), clearSemanticApplyAttempts(AsyncStorage, origin), clearMultiAttempts(AsyncStorage, origin), clearLensAttempts(AsyncStorage, origin), clearMultiApplyAttempts(AsyncStorage, origin), clearPerspectiveApplyAttempts(AsyncStorage, origin), clearPerspectiveLinesAttempts(AsyncStorage, origin), clearPerspectiveAttempts(AsyncStorage, origin), clearNegativeAttempts(AsyncStorage, origin), clearRelightAnalysisAttempts(AsyncStorage, origin), clearRelightAttempts(AsyncStorage, origin), clearRelightReviewDecisionAttempts(AsyncStorage, origin), clearGlareDonorAttempts(AsyncStorage, origin), clearOpenEyesAttempts(AsyncStorage, origin), clearReflectionAttempts(AsyncStorage, origin), clearLocalPortraitAttempts(AsyncStorage, origin), clearSemanticAttempts(AsyncStorage, origin), clearSceneApplyAttempts(AsyncStorage, origin), clearSceneReferenceAttempts(AsyncStorage, origin), clearSessionAnalysisAttempts(AsyncStorage, origin), clearContactSheetAttempts(AsyncStorage, origin), clearCollectionCreateAttempts(AsyncStorage, origin), clearCaptureDrafts(AsyncStorage, origin), clearSessionPolicyDrafts(AsyncStorage, origin), clearEditorDraftCaches(AsyncStorage, origin), clearEditorRenderCaches(AsyncStorage, origin), clearEditorRecipeBridges(AsyncStorage, origin), clearEditorEffectsAttempts(AsyncStorage, origin), clearCullingDecisionAttempts(AsyncStorage, origin)]); }), []);
    const resource = useReadResource('owner-auth-restoration', auth, signal => auth.restore(signal), { foreground: false });
    const { t } = useLocale();
    const clearing = useRef(false), [clearingBusy, setClearingBusy] = useState(false);
    if (resource.phase !== 'ready')
        return <View style={{ flex: 1, backgroundColor: '#222', justifyContent: 'center' }}><ReadResourceState {...resource} retry={() => { if (!clearing.current)
            resource.retry(); }} cancel={() => { if (!clearing.current)
            resource.cancel(); }}/>{resource.phase !== 'loading' && resource.phase !== 'paused' ? <Button accessibilityRole="button" disabled={clearingBusy} onPress={() => { if (clearing.current)
            return; clearing.current = true; setClearingBusy(true); void auth.clear().then(() => resource.commit(false)).catch(() => { }).finally(() => { clearing.current = false; if (resource.current())
            setClearingBusy(false); }); }} mode="text">{t('auth_continue_login')}</Button> : null}</View>;
    return <AuthContext.Provider value={auth}>{children}</AuthContext.Provider>;
}
export function useAuth() {
    const value = useContext(AuthContext);
    if (!value)
        throw new Error("AuthProvider missing");
    const generation = useSyncExternalStore(value.subscribeSession, value.sessionGeneration, value.sessionGeneration);
    return useMemo(() => new Proxy(value, { get(target, key) { const member = Reflect.get(target, key, target); if (typeof member !== 'function')
            return member; return (...args: unknown[]) => { if (target.sessionGeneration() !== generation)
            throw new DOMException("Auth session changed", "AbortError"); return Reflect.apply(member, target, args); }; } }), [value, generation]);
}
