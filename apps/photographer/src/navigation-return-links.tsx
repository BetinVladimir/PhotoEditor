import {Button, useTheme} from 'react-native-paper';
import { Text, TouchableRipple as Pressable } from 'react-native-paper';
import { useLocale } from './locale-context';
import { useEffect, useRef } from 'react';
import { router } from './localized-router';
import { View } from 'react-native';
import { scopeUUID } from './workspace-access';
import { useReviewForeground } from './use-review-foreground';
/** A direct entry has no guaranteed browser/native back stack. These links do
 * not discard journals or replay the command that opened the current screen. */
export function NavigationReturnLinks({ tenant, current = () => true }: {
    tenant?: string;
    current?: () => boolean;
}) {
    const { t } = useLocale();
    const theme = useTheme();
    const foreground = useReviewForeground(), alive = useRef(true);
    useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
    const authority = useRef({ tenant, current, active: foreground.active });
    authority.current = { tenant, current, active: foreground.active };
    const own = () => alive.current && authority.current.tenant === tenant && authority.current.active && current();
    return <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 16, padding: 12, backgroundColor: theme.colors.background }}>
  <Button accessibilityRole="button" onPress={() => { if (own())
        router.push('/workspaces'); }} mode="text">{t("nav_workspaces")}</Button>
  {scopeUUID(tenant) ? <Button accessibilityRole="button" onPress={() => { if (own())
        router.push({ pathname: '/projects', params: { tenant } }); }} mode="text">{t("nav_projects")}</Button> : null}
 </View>;
}
