import { buildFlowUrl, parseFlowUrl } from './ConfigEditor';

describe('Flow Server address', () => {
  const id = '7828D35F-D2C7-4D09-AF8C-333C046F1A77';

  it('is built from host, port and instance', () => {
    expect(buildFlowUrl({ https: false, host: 'flow-server', port: '', instanceId: id })).toBe(
      `http://flow-server:4501/flow-software/flow/instances/${id}/server`
    );
    expect(buildFlowUrl({ https: true, host: ' flow ', port: '8443', instanceId: `{${id}}` })).toBe(
      `https://flow:8443/flow-software/flow/instances/${id}/server`
    );
    // Incomplete: no address yet.
    expect(buildFlowUrl({ https: false, host: 'flow', port: '', instanceId: '' })).toBe('');
  });

  it('is split back into its parts, Swagger addresses included', () => {
    expect(parseFlowUrl(`http://host.docker.internal:4501/flow-software/flow/instances/${id}/server`)).toEqual({
      https: false,
      host: 'host.docker.internal',
      port: '4501',
      instanceId: id,
    });
    expect(parseFlowUrl(`HTTPS://flow/flow-software/flow/instances/${id}/server/api/`)).toEqual({
      https: true,
      host: 'flow',
      port: '',
      instanceId: id,
    });
    expect(parseFlowUrl('https://proxy.example.com/flow/api')).toBeUndefined();
  });
});
