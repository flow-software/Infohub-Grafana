import { test, expect } from '@grafana/plugin-e2e';

const URL_PLACEHOLDER = 'http://flow-server:4501/flow-software/flow/instances/<InstanceID>/server';

test('smoke: should render config editor', async ({ createDataSourceConfigPage, readProvisionedDataSource, page }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await createDataSourceConfigPage({ type: ds.type });
  await expect(page.getByPlaceholder(URL_PLACEHOLDER)).toBeVisible();
});

test('"Save & test" should fail and name the missing setting', async ({ createDataSourceConfigPage, page }) => {
  const configPage = await createDataSourceConfigPage({ type: 'flowsoftware-flow-datasource' });
  await page.getByPlaceholder(URL_PLACEHOLDER).fill('http://flow-server:4501/flow-software/flow/instances/x/server');
  await expect(configPage.saveAndTest()).not.toBeOK();
  await expect(configPage).toHaveAlert('error', { hasText: 'API token is required' });
});
