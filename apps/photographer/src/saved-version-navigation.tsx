import {Button} from 'react-native-paper';
import { Text, TouchableRipple as Pressable } from 'react-native-paper';
import { useLocale } from './locale-context';
import { useEffect, useRef, useState } from 'react';
import { router } from './localized-router';
import { View } from 'react-native';
import type { OwnerAPI } from './owner-api';
import { versionEditorTarget } from './version-navigation';
import { readDeadline } from './read-deadline';
import { useWorkspaceAccess } from './workspace-access-context';
import { useReviewForeground } from './use-review-foreground';
export function SavedVersionNavigation({ api, tenant, project, version }: {
    api: OwnerAPI;
    tenant: string;
    project: string;
    version: string;
}) {
    const { t } = useLocale();
    const access = useWorkspaceAccess(tenant), foreground = useReviewForeground(), [busy, setBusy] = useState(false), [failed, setFailed] = useState(false), flight = useRef<AbortController | null>(null);
    const scope = JSON.stringify([tenant, project, version, access?.actor, access?.canEdit, foreground.generation]);
    const owner = useRef({ api, scope, active: foreground.active });
    owner.current = { api, scope, active: foreground.active };
    useEffect(() => { setBusy(false); setFailed(false); return () => { flight.current?.abort(); flight.current = null; }; }, [api, scope, foreground.active]);
    async function open() {
        if (!access?.canEdit || !foreground.active || flight.current)
            return;
        const controller = new AbortController();
        flight.current = controller;
        setBusy(true);
        setFailed(false);
        const current = () => !controller.signal.aborted && flight.current === controller && owner.current.api === api && owner.current.scope === scope && owner.current.active;
        try {
            const target = await readDeadline(controller.signal, signal => versionEditorTarget(api, tenant, project, version, access.actor, signal, current));
            if (current())
                router.push(target);
        }
        catch {
            if (current())
                setFailed(true);
        }
        finally {
            if (flight.current === controller) {
                flight.current = null;
                setBusy(false);
            }
        }
    }
    return <View><Button accessibilityRole="button" disabled={!access?.canEdit || !foreground.active || busy} onPress={() => void open()} mode="text">{t("nav_open_version")}</Button>{busy ? <Button accessibilityRole="button" onPress={() => { flight.current?.abort(); flight.current = null; setBusy(false); setFailed(true); }} mode="text">{t("nav_cancel_opening")}</Button> : null}{failed ? <Text accessibilityRole="alert">{t("nav_open_failed")}</Text> : null}</View>;
}
