import { expect, test } from '@playwright/test';
import { owner, signIn } from '../helpers/helpers.ts';

// The lab qBittorrent's address as the app sees it, e.g. http://192.168.1.10:18708.
const LAB_QBIT = process.env.HOLYARRS_LAB_QBIT_URL ?? '';

test('the Downloaders Test button reaches the preserved lab qBittorrent with its API key', async ({ page }) => {
	test.skip(!LAB_QBIT, 'needs the lab qBittorrent running and its address in HOLYARRS_LAB_QBIT_URL');
	// A qBittorrent is saved with an API key or a sign-in, never neither (a3301b5d).
	const key = process.env.HOLYARRS_LAB_QBIT_KEY ?? '';
	test.skip(!key, 'needs the lab qBittorrent API key in HOLYARRS_LAB_QBIT_KEY');
	const versionReply = await fetch(`${LAB_QBIT}/api/v2/app/version`, {
		headers: { Authorization: `Bearer ${key}` },
		signal: AbortSignal.timeout(3000)
	});
	expect(versionReply.ok, 'the lab qBittorrent accepts its configured API key').toBe(true);
	const version = (await versionReply.text()).trim();
	expect(version).toMatch(/^v\d/);
	await signIn(page, owner());
	await page.goto('/settings/downloaders');
	await page.getByRole('button', { name: 'Add a Downloader' }).first().click();
	await page.getByRole('radio', { name: /qBittorrent/ }).check();
	await page.getByLabel('Name', { exact: true }).fill('Preserved lab qBittorrent');
	await page.getByLabel('Address', { exact: true }).fill(LAB_QBIT);
	await expect(page.getByRole('radio', { name: 'API key' })).toBeChecked();
	await page.getByRole('textbox', { name: 'API key', exact: true }).fill(key);
	await page.getByText('More options').click();
	await page.getByLabel('Category', { exact: true }).fill('holyarrs-acceptance');
	await page.getByLabel('Reach the local network').check();
	await page.locator('form.sc-form').first().getByRole('button', { name: 'Save', exact: true }).click();

	const downloader = page.getByTestId('downloader').filter({ hasText: 'Preserved lab qBittorrent' });
	await expect(downloader.getByTestId('downloader-version')).toHaveText(`qBittorrent ${version}`);
	await expect(downloader.getByTestId('downloader-state')).toHaveText('Working');
	await expect(downloader).toContainText('may reach the local network');
	await expect(downloader).not.toContainText('password');

	await downloader.hover(); // row buttons show on hover
	const tested = page.waitForResponse((response) => response.request().method() === 'POST' && /\/api\/v1\/downloaders\/\d+\/test$/.test(new URL(response.url()).pathname));
	await downloader.getByRole('button', { name: 'Test', exact: true }).click();
	const testReply = await tested;
	expect(testReply.ok()).toBe(true);
	expect(await testReply.json()).toMatchObject({ ok: true });
	await expect(downloader.getByTestId('downloader-version')).toHaveText(`qBittorrent ${version}`);
	await expect(downloader.getByTestId('downloader-state')).toHaveText('Working');
});
