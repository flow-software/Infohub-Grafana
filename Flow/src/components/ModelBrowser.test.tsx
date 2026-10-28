import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import { DataSource } from '../datasource';
import { BrowseNode, QueryItem } from '../types';
import { ModelBrowser } from './ModelBrowser';

const nodes: Record<string, BrowseNode[]> = {
  'model:root': [{ tree: 'model', type: 'folder', id: 1, name: 'Site' }],
  'model:folder:1': [{ tree: 'model', type: 'metric', id: 20, name: 'Production', fromTemplate: true }],
  'model:metric:20': [
    { tree: 'model', type: 'measure', id: 200, name: 'Good', interval: 'hourly', uom: 'units', retrieval: 'manual' },
    { tree: 'model', type: 'measure', id: 201, name: 'Speed', interval: 'timestamp', retrieval: 'retrieved' },
  ],
  'template:root': [{ tree: 'template', type: 'folder', id: 90, name: 'Line' }],
};

function fakeDatasource() {
  return {
    browse: jest.fn((tree: string, parent?: { type: string; id: number }) =>
      Promise.resolve(nodes[parent ? `${tree}:${parent.type}:${parent.id}` : `${tree}:root`] ?? [])
    ),
    search: jest.fn(() => Promise.resolve([])),
    getSummary: jest.fn(() => Promise.resolve({ periodMeasures: 3, tags: 1 })),
  } as unknown as DataSource;
}

describe('ModelBrowser', () => {
  it('loads levels on demand and labels picks with their path', async () => {
    const ds = fakeDatasource();
    const onApply = jest.fn();
    const existing: QueryItem = { tree: 'template', type: 'measure', id: 9000, label: 'Line/Production/Good' };
    render(<ModelBrowser datasource={ds} selected={[existing]} onApply={onApply} onDismiss={() => {}} />);

    // The existing pick is a template item, so the Templates tab opens first.
    expect(await screen.findByText('Line')).toBeInTheDocument();
    fireEvent.click(screen.getByText('Model'));

    fireEvent.click(await screen.findByLabelText('Expand Site'));
    fireEvent.click(await screen.findByLabelText('Expand Production'));
    expect(await screen.findByText('tag')).toBeInTheDocument();
    expect(screen.getByText('hourly')).toBeInTheDocument();
    // Measures show their id, to match what Flow and the node graph show.
    expect(screen.getByText('#200')).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText('Select Speed'));
    fireEvent.click(screen.getByLabelText('Select Production'));
    fireEvent.click(screen.getByText('Apply'));

    await waitFor(() => expect(onApply).toHaveBeenCalled());
    expect(onApply.mock.calls[0][0]).toEqual([
      existing,
      {
        tree: 'model',
        type: 'measure',
        id: 201,
        label: 'Site/Production/Speed',
        interval: 'timestamp',
        retrieval: 'retrieved',
      },
      { tree: 'model', type: 'metric', id: 20, label: 'Site/Production' },
    ]);
    // Each level was fetched once.
    expect(ds.browse).toHaveBeenCalledTimes(4);
  });

  it('narrows the measures to the picked intervals', async () => {
    render(<ModelBrowser datasource={fakeDatasource()} selected={[]} onApply={() => {}} onDismiss={() => {}} />);
    fireEvent.click(await screen.findByLabelText('Expand Site'));
    fireEvent.click(await screen.findByLabelText('Expand Production'));
    expect(await screen.findByText('Speed')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /Hourly/ }));
    expect(screen.getByText('Good')).toBeInTheDocument();
    expect(screen.queryByText('Speed')).not.toBeInTheDocument();
    // Folders and metrics stay: what they hold is only known once opened.
    expect(screen.getByText('Production')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /Hourly/ }));
    fireEvent.click(screen.getByRole('button', { name: /Yearly/ }));
    expect(screen.getByText('No yearly measures here')).toBeInTheDocument();

    fireEvent.click(screen.getAllByRole('button', { name: 'All' })[0]);
    expect(screen.getByText('Speed')).toBeInTheDocument();
  });

  it('narrows the measures to the picked retrieval types, together with intervals', async () => {
    render(<ModelBrowser datasource={fakeDatasource()} selected={[]} onApply={() => {}} onDismiss={() => {}} />);
    fireEvent.click(await screen.findByLabelText('Expand Site'));
    fireEvent.click(await screen.findByLabelText('Expand Production'));
    expect(await screen.findByText('Speed')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /Retrieved/ }));
    expect(screen.getByText('Speed')).toBeInTheDocument();
    expect(screen.queryByText('Good')).not.toBeInTheDocument();

    // Both filters apply: no hourly measure is retrieved.
    fireEvent.click(screen.getByRole('button', { name: /Hourly/ }));
    expect(screen.getByText('No hourly retrieved measures here')).toBeInTheDocument();
  });

  it('picks a whole folder, saying how many measures it holds', async () => {
    const onApply = jest.fn();
    render(<ModelBrowser datasource={fakeDatasource()} selected={[]} onApply={onApply} onDismiss={() => {}} />);
    fireEvent.click(await screen.findByLabelText('Select Site'));
    expect(await screen.findByText('4 measures')).toBeInTheDocument();
    fireEvent.click(screen.getByText('Apply'));
    expect(onApply.mock.calls[0][0]).toEqual([{ tree: 'model', type: 'folder', id: 1, label: 'Site' }]);
  });

  it('does not offer folders when browsing events', async () => {
    render(
      <ModelBrowser datasource={fakeDatasource()} selected={[]} onApply={() => {}} onDismiss={() => {}} kind="events" />
    );
    await screen.findByText('Site');
    expect(screen.queryByLabelText('Select Site')).not.toBeInTheDocument();
  });

  it('unticking removes an existing pick', async () => {
    const ds = fakeDatasource();
    const onApply = jest.fn();
    const existing: QueryItem = { tree: 'model', type: 'metric', id: 20, label: 'Site/Production' };
    render(<ModelBrowser datasource={ds} selected={[existing]} onApply={onApply} onDismiss={() => {}} />);

    fireEvent.click(await screen.findByLabelText('Expand Site'));
    fireEvent.click(await screen.findByLabelText('Select Production'));
    fireEvent.click(screen.getByText('Apply'));

    expect(onApply).toHaveBeenCalledWith([]);
  });
});
