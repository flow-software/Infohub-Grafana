import React, { FormEvent, useEffect, useState } from 'react';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import { Combobox, ComboboxOption, Field, FieldSet, Input, SecretInput, SecretTextArea, Switch } from '@grafana/ui';

import { Calendar, FlowOptions, FlowSecureJsonData } from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<FlowOptions, FlowSecureJsonData> {}

const URL_PLACEHOLDER = 'http://flow-server:4501/flow-software/flow/instances/<InstanceID>/server';

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

  const calendarOptions: Array<ComboboxOption<number>> = [
    { label: 'First calendar in Flow', value: 0 },
    ...(calendars ?? []).map((c) => ({ label: c.name, value: c.id, description: c.description })),
  ];

  return (
    <>
      <FieldSet label="Connection">
        <Field
          label="URL"
          description="The Flow Server API through Flow Bootstrap: the Bootstrap address, then /flow-software/flow/instances/, the instance ID (a GUID, not the instance name), then /server."
          required
        >
          <Input
            id="config-url"
            width={80}
            value={options.url}
            placeholder={URL_PLACEHOLDER}
            onChange={(e) => onOptionsChange({ ...options, url: e.currentTarget.value })}
          />
        </Field>
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
      .get<Calendar[]>(`/api/datasources/uid/${uid}/resources/calendars`, undefined, undefined, { showErrorAlert: false })
      .then((list) => !cancelled && setCalendars(list))
      .catch(() => !cancelled && setCalendars(undefined));
    return () => {
      cancelled = true;
    };
  }, [uid, version]);
  return calendars;
}
