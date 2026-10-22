import {Button} from 'react-native-paper';
import { Text, TouchableRipple as Pressable } from 'react-native-paper';
import { useLocale } from './locale-context';
import { router } from './localized-router';
import { View } from 'react-native';
import { profileTarget, workspaceSettingsTargets, sessionAnalysisTarget } from './navigation-targets';
import { useWorkspaceAccess } from './workspace-access-context';
import { catalogStyle as s } from './catalog-style';
export function ProfileNavigation() { const { t } = useLocale(); return <Button accessibilityRole="button" onPress={() => router.push(profileTarget)} mode="contained">{t("nav_profile")}</Button>; }
export function WorkspaceSettingsNavigation({ tenant }: {
    tenant: string;
}) {
    const { t } = useLocale();
    const access = useWorkspaceAccess(tenant);
    if (!access)
        return null;
    return <View>{workspaceSettingsTargets(tenant).map(item => <Button key={item.href.pathname} accessibilityRole="button" onPress={() => router.push(item.href)} mode="contained">{t(item.labelKey)}</Button>)}</View>;
}
export function SessionAnalysisNavigation({ tenant, id, project, disabled = false }: {
    tenant: string;
    id: string;
    project: string;
    disabled?: boolean;
}) {
    const { t } = useLocale();
    const access = useWorkspaceAccess(tenant);
    if (!access)
        return null;
    const target = sessionAnalysisTarget(tenant, id, project);
    return <Button accessibilityRole="button" disabled={disabled} onPress={() => router.push(target)} mode="contained">{t("nav_session_analysis")}</Button>;
}
