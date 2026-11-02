import React, { FormEvent, useEffect, useState } from 'react';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import {
  Button,
  Combobox,
  ComboboxOption,
  Field,
  FieldSet,
  InlineField,
  InlineSwitch,
  Input,
  SecretInput,
  SecretTextArea,
  Stack,
  Switch,
} from '@grafana/ui';

import { Calendar, FlowOptions, FlowSecureJsonData } from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<FlowOptions, FlowSecureJsonData> {}

const URL_PLACEHOLDER = 'http://flow-server:4501/flow-software/flow/instances/<InstanceID>/server';

/** Flow Bootstrap's default port. */
export const DEFAULT_PORT = '4501';

/** The parts of a Flow Server address: Bootstrap's host and port, and the instance. */
export interface FlowAddress {
  https: boolean;
  host: string;
  port: string;
  instanceId: string;
}

const FLOW_URL =
  /^(https?):\/\/([^/:?#]+)(?::(\d+))?\/flow-software\/flow\/instances\/([^/?#]+)\/server(?:\/api(?:\/v1)?)?\/?$/i;
const GUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Splits a Flow Server (or Swagger) address into its parts; undefined if it is not one. */
export function parseFlowUrl(url: string | undefined): FlowAddress | undefined {
  const m = FLOW_URL.exec((url ?? '').trim());
  if (!m) {
    return undefined;
  }
  return { https: m[1].toLowerCase() === 'https', host: m[2], port: m[3] ?? '', instanceId: m[4] };
}

/** The Flow Server address for the parts; empty until host and instance are given. */
export function buildFlowUrl(a: FlowAddress): string {
  const host = a.host.trim();
  const id = a.instanceId.trim().replace(/^\{|\}$/g, '');
  if (!host || !id) {
    return '';
  }
  return `${a.https ? 'https' : 'http'}://${host}:${a.port.trim() || DEFAULT_PORT}/flow-software/flow/instances/${id}/server`;
}

export function ConfigEditor({ options, onOptionsChange }: Props) {
  const { jsonData, secureJsonFields } = options;
  const secureJsonData = options.secureJsonData ?? {};
  const calendars = useSavedCalendars(options.uid, options.version);

  const setJson = <K extends keyof FlowOptions>(key: K, value: FlowOptions[K]) =>
    onOptionsChange({ ...options, jsonData: { ...jsonData, [key]: value } });

  const setSecret = (key: keyof FlowSecureJsonData) => (e: FormEvent<HTMLInputElement | HTMLTextAreaElement>) =>
    onOptionsChange({ ...options, secureJsonData: { ...secureJsonData, [key]: e.currentTarget.value } });

  const resetSecret = (key: keyof FlowSecureJsonData) => () =>
    onOptionsChange({
      ...options,
      secureJsonFields: { ...secureJsonFields, [key]: false },
      secureJsonData: { ...secureJsonData, [key]: '' },
    });

  // The address is kept as one URL, which the backend reads; it is edited as
  // its parts, unless it is something else (a reverse proxy, say).
  const [custom, setCustom] = useState(() => !!options.url && !parseFlowUrl(options.url));
  const [address, setAddress] = useState<FlowAddress>(
    () => parseFlowUrl(options.url) ?? { https: false, host: '', port: '', instanceId: '' }
  );
  const setPart = (patch: Partial<FlowAddress>) => {
    let next = { ...address, ...patch };
    // A whole Flow or Swagger address pasted into Host fills every part.
    if (patch.host && patch.host.includes('://')) {
      const parsed = parseFlowUrl(patch.host);
      if (parsed) {
        next = parsed;
      } else {
        try {
          const u = new URL(patch.host);
          next = { ...next, https: u.protocol === 'https:', host: u.hostname, port: u.port };
        } catch {
          // Not a URL after all: keep what was typed.
        }
      }
    }
    setAddress(next);
    onOptionsChange({ ...options, url: buildFlowUrl(next) });
  };
  const idText = address.instanceId.trim().replace(/^\{|\}$/g, '');
  const badId = idText !== '' && !GUID.test(idText);

  const calendarOptions: Array<ComboboxOption<number>> = [
    { label: 'First calendar in Flow', value: 0 },
    ...(calendars ?? []).map((c) => ({ label: c.name, value: c.id, description: c.description })),
  ];

  return (
    <>
      <FieldSet label="Connection">
        {custom ? (
          <Field
            label="URL"
            description="The Flow Server API through Flow Bootstrap, or through a proxy in front of it."
            required
          >
            <Stack gap={1} alignItems="center">
              <Input
                id="config-url"
                width={80}
                value={options.url}
                placeholder={URL_PLACEHOLDER}
                onChange={(e) => onOptionsChange({ ...options, url: e.currentTarget.value })}
              />
              <Button
                variant="secondary"
                fill="text"
                onClick={() => {
                  const parsed = parseFlowUrl(options.url);
                  if (parsed) {
                    setAddress(parsed);
                  }
                  setCustom(false);
                }}
              >
                Use host and instance ID
              </Button>
            </Stack>
          </Field>
        ) : (
          <>
            <Field
              label="Host"
              description="The machine running Flow Bootstrap, as the Grafana server reaches it. You can also paste a Flow Server or Swagger address here."
              required
            >
              <Stack gap={1} alignItems="center">
                <Input
                  id="config-host"
                  width={40}
                  value={address.host}
                  placeholder="flow-server"
                  onChange={(e) => setPart({ host: e.currentTarget.value })}
                />
                <InlineField label="Port" tooltip={`Flow Bootstrap's port; empty uses ${DEFAULT_PORT}.`}>
                  <Input
                    id="config-port"
                    width={10}
                    value={address.port}
                    placeholder={DEFAULT_PORT}
                    onChange={(e) => setPart({ port: e.currentTarget.value.replace(/[^0-9]/g, '') })}
                  />
                </InlineField>
                <InlineSwitch
                  id="config-https"
                  label="HTTPS"
                  showLabel
                  value={address.https}
                  onChange={(e) => setPart({ https: e.currentTarget.checked })}
                />
              </Stack>
            </Field>
            <Field
              label="Instance ID"
              description="The Flow instance's ID, a GUID such as 7828D35F-D2C7-4D09-AF8C-333C046F1A77 (not its name). It is part of the Flow Server address in Flow Config and of the Swagger page's address."
              required
              invalid={badId}
              error={badId ? 'An instance ID is a GUID: 8-4-4-4-12 hexadecimal characters.' : undefined}
            >
              <Input
                id="config-instance"
                width={50}
                value={address.instanceId}
                placeholder="00000000-0000-0000-0000-000000000000"
                onChange={(e) => setPart({ instanceId: e.currentTarget.value })}
              />
            </Field>
            <Field label="Address" description="What the plugin connects to, built from the fields above.">
              <Stack gap={1} alignItems="center" wrap="wrap">
                <code>{options.url || 'Enter the host and instance ID'}</code>
                <Button variant="secondary" fill="text" onClick={() => setCustom(true)}>
                  Enter a full URL instead
                </Button>
              </Stack>
            </Field>
          </>
        )}
        <Field
          label="API token"
          description="A Flow server token, created in Flow Config. Its access group decides which folders Grafana can see."
          required
        >
          <SecretInput
            id="config-token"
            width={80}
            isConfigured={!!secureJsonFields.token}
            value={secureJsonData.token ?? ''}
            onChange={setSecret('token')}
            onReset={resetSecret('token')}
          />
        </Field>
      </FieldSet>

      <FieldSet label="Defaults">
        <Field
          label="Default calendar"
          description={
            calendars
              ? 'Period measures (hourly, shift, daily, ...) are read in this calendar unless a query picks another.'
              : 'Save & test to choose from the calendars in Flow. Until then, enter a calendar ID, or 0 for the first calendar.'
          }
        >
          {calendars ? (
            <Combobox
              id="config-default-calendar"
              width={40}
              options={calendarOptions}
              value={jsonData.defaultCalendarId ?? 0}
              onChange={(o) => setJson('defaultCalendarId', o?.value || undefined)}
            />
          ) : (
            <Input
              id="config-default-calendar"
              type="number"
              min={0}
              width={20}
              value={jsonData.defaultCalendarId ?? 0}
              onChange={(e) => setJson('defaultCalendarId', Number(e.currentTarget.value) || undefined)}
            />
          )}
        </Field>
      </FieldSet>

      <FieldSet label="TLS">
        <Field
          label="Skip certificate verification"
          description="Accept a self-signed Bootstrap certificate. Avoid in production."
        >
          <Switch
            id="config-tls-skip-verify"
            value={!!jsonData.tlsSkipVerify}
            onChange={(e) => setJson('tlsSkipVerify', e.currentTarget.checked)}
          />
        </Field>
        <Field label="Custom CA certificate" description="Trust a Bootstrap certificate signed by this CA.">
          <Switch
            id="config-tls-ca"
            value={!!jsonData.tlsAuthWithCACert}
            onChange={(e) => setJson('tlsAuthWithCACert', e.currentTarget.checked)}
          />
        </Field>
        {jsonData.tlsAuthWithCACert && (
          <Field label="CA certificate (PEM)">
            <SecretTextArea
              id="config-tls-ca-cert"
              cols={60}
              rows={6}
              isConfigured={!!secureJsonFields.tlsCACert}
              value={secureJsonData.tlsCACert ?? ''}
              placeholder="-----BEGIN CERTIFICATE-----"
              onChange={setSecret('tlsCACert')}
              onReset={resetSecret('tlsCACert')}
            />
          </Field>
        )}
      </FieldSet>
    </>
  );
}

/**
 * Calendars from the saved datasource, or undefined while it cannot answer
 * (new, unsaved, or not yet working). Reloaded after each save.
 */
function useSavedCalendars(uid: string | undefined, version: number | undefined): Calendar[] | undefined {
  const [calendars, setCalendars] = useState<Calendar[]>();
  useEffect(() => {
    if (!uid) {
      return;
    }
    let cancelled = false;
    getBackendSrv()
      .get<Calendar[]>(`/api/datasources/uid/${uid}/resources/calendars`, undefined, undefined, {
        showErrorAlert: false,
      })
      .then((list) => !cancelled && setCalendars(list))
      .catch(() => !cancelled && setCalendars(undefined));
    return () => {
      cancelled = true;
    };
  }, [uid, version]);
  return calendars;
}
