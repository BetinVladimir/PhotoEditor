import {Button} from 'react-native-paper';
import { Text, TextInput, TouchableRipple as Pressable } from 'react-native-paper';
import { useEffect, useRef, useState } from 'react';
import { View } from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type { ApiClient } from '@photoeditor/api-client';
import { CatalogCreateAttemptStore, type CatalogCreateAttempt } from './catalog-create-attempt-store';
import { readDeadline } from './read-deadline';
import { useReadResource } from './use-read-resource';
import { ReadResourceState } from './read-resource-state';
import { useLocale } from './locale-context';
import { catalogStyle as s } from './catalog-style';
import { readProject, readStyleProfile } from './catalog-read-response';
/** Cancellation retains the immutable command for exact retry. */
export function CatalogCreateControls({ client, origin, tenant, project = null, kind, enabled, current, onCreated }: {
    client: ApiClient;
    origin: string;
    tenant: string;
    project?: string | null;
    kind: 'project' | 'style';
    enabled: boolean;
    current: () => boolean;
    onCreated: (value: unknown) => void;
}) {
    const { t } = useLocale(), [name, setName] = useState(''), [attempt, setAttempt] = useState<CatalogCreateAttempt | null>(null), [busy, setBusy] = useState(false), [failed, setFailed] = useState(false);
    const pending = useRef<CatalogCreateAttempt | null>(null), flight = useRef<AbortController | null>(null), flightCurrent = useRef<(() => boolean) | null>(null);
    const authority = useRef({ current, enabled });
    authority.current = { current, enabled };
    const scope = JSON.stringify([origin, tenant, project, kind]);
    const resource = useReadResource(scope, client, async (signal) => {
        const user = await client.request<{
            id: string;
        }>('/api/v1/me', { signal });
        signal.throwIfAborted();
        const store = new CatalogCreateAttemptStore(AsyncStorage, origin, user.id, tenant, kind, project), saved = await store.read();
        signal.throwIfAborted();
        return { store, saved };
    });
    useEffect(() => { if (resource.phase === 'ready') {
        pending.current = resource.data!.saved;
        setAttempt(pending.current);
        if (pending.current)
            setName(pending.current.request.name);
    } }, [resource.data, resource.phase]);
    useEffect(() => { setBusy(false); return () => { flight.current?.abort(); flight.current = null; }; }, [client, scope, enabled, resource.phase]);
    useEffect(() => { if (flight.current && flightCurrent.current && !flightCurrent.current()) {
        flight.current.abort();
        flight.current = null;
        flightCurrent.current = null;
        setBusy(false);
    } });
    function cancel() { flight.current?.abort(); flight.current = null; setBusy(false); setFailed(true); }
    const base = `/api/v1/tenants/${encodeURIComponent(tenant)}/${kind === 'project' ? 'projects' : 'styles'}`;
    function validate(value: unknown, id?: string) {
        return kind === 'project' ? readProject(value, tenant, id, id !== undefined) : readStyleProfile(value, tenant, { id, project: project ?? undefined });
    }
    async function create() {
        const store = resource.data?.store;
        if (!store || resource.phase !== 'ready' || !authority.current.current() || flight.current || !authority.current.enabled)
            return;
        if (!pending.current) {
            const trimmed = name.trim();
            if (!trimmed || Array.from(trimmed).length > 120) {
                setFailed(true);
                return;
            }
            pending.current = { schema_version: 1, key: crypto.randomUUID(), request: { name: trimmed }, ticket: null };
            setAttempt(pending.current);
        }
        const parentCurrent = current, resourceCurrent = resource.current;
        const frozen = pending.current, controller = new AbortController();
        flight.current = controller;
        setBusy(true);
        setFailed(false);
        const own = () => flight.current === controller && !controller.signal.aborted && parentCurrent() && resourceCurrent() && authority.current.enabled;
        flightCurrent.current = own;
        try {
            await readDeadline(controller.signal, async (signal) => {
                await store.write(frozen);
                signal.throwIfAborted();
                if (!own())
                    throw new Error('CATALOG_CREATE_CANCELLED');
                let accepted = frozen;
                if (!accepted.ticket) {
                    const body = kind === 'project' ? { name: frozen.request.name, description: '', visibility: 'workspace', download_allowed: true } : { project_id: project, name: frozen.request.name, description: '' };
                    const row = validate(await client.request<unknown>(base, { method: 'POST', body, idempotencyKey: frozen.key, signal }));
                    signal.throwIfAborted();
                    if (!own())
                        return;
                    accepted = { ...frozen, ticket: { resource_id: row.id } };
                    pending.current = accepted;
                    setAttempt(accepted);
                    await store.write(accepted);
                    signal.throwIfAborted();
                }
                const row = await client.request<unknown>(`${base}/${encodeURIComponent(accepted.ticket!.resource_id)}`, { signal });
                signal.throwIfAborted();
                if (!own())
                    return;
                const confirmed = validate(row, accepted.ticket!.resource_id);
                onCreated(confirmed);
                await store.clearAccepted(accepted.key, accepted.ticket!.resource_id);
                signal.throwIfAborted();
                if (!own())
                    return;
                pending.current = null;
                setAttempt(null);
                setName('');
            }, 15000);
        }
        catch {
            if (own())
                setFailed(true);
        }
        finally {
            if (flight.current === controller) {
                flight.current = null;
                setBusy(false);
            }
        }
    }
    return <View><ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel}/><View style={s.toolbar}><TextInput accessibilityLabel={t(kind === 'project' ? 'catalog_project_name' : 'catalog_style_name')} editable={enabled && !busy && !attempt} value={name} onChangeText={setName} style={s.input}/><Button accessibilityRole="button" disabled={!enabled || busy || resource.phase !== 'ready' || !name.trim()} onPress={() => void create()} mode="contained">{t(attempt ? 'catalog_recover_create' : 'catalog_create')}</Button></View>{busy ? <Button accessibilityRole="button" onPress={cancel} mode="text">{t('read_cancel')}</Button> : null}{failed ? <Text accessibilityRole="alert">{t('catalog_create_uncertain')}</Text> : null}</View>;
}
