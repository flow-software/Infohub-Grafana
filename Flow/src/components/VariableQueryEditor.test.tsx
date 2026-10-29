import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';

import type { DataSource } from '../datasource';
import { VariableQueryEditor } from './VariableQueryEditor';

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: () => ({ getVariables: () => [] }),
}));

const datasource = { getAttributes: () => Promise.resolve([]) } as unknown as DataSource;

describe('VariableQueryEditor', () => {
  it('switches between the lists of a group', () => {
    const onChange = jest.fn();
    render(
      <VariableQueryEditor
        datasource={datasource}
        query={{ refId: 'v', queryType: 'variable', variableKind: 'attributeNames' }}
        onChange={onChange}
        onRunQuery={() => undefined}
      />
    );
    fireEvent.click(screen.getByLabelText('Values of an attribute'));
    expect(onChange).toHaveBeenCalledWith({ refId: 'v', queryType: 'variable', variableKind: 'attributeValues' });
  });

  it('starts a group on its first list', () => {
    const onChange = jest.fn();
    render(
      <VariableQueryEditor
        datasource={datasource}
        query={{ refId: 'v', queryType: 'variable', variableKind: 'charts' }}
        onChange={onChange}
        onRunQuery={() => undefined}
      />
    );
    fireEvent.click(screen.getByLabelText('Events'));
    expect(onChange).toHaveBeenCalledWith({ refId: 'v', queryType: 'variable', variableKind: 'events' });
  });
});
