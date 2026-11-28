import { expect, test } from '@playwright/test';
import { noSidewaysScroll, owner, signIn } from '../helpers/helpers.ts';

test('Watch History keeps an entry when deletion fails and removes it on retry', async ({ page }) => {
	await signIn(page, owner());
	const me = await page.request.get('/api/v1/auth/me');
	const { csrf } = (await me.json()) as { csrf: string };
	const name = 'History deletion fixture';
	const saved = await page.request.put('/api/v1/stream/history', {
		headers: { 'X-CSRF-Token': csrf },
		data: { kind: 'movie', tmdb: '900999', title: name, position_sec: 60, duration_sec: 120 }
	});
	expect(saved.ok()).toBe(true);
	const { key, completed } = (await saved.json()) as { key: string; completed: boolean };
	expect(completed).toBe(false);
	const path = `**/api/v1/stream/history/${encodeURIComponent(key)}`;
	await page.route(path, (route) => route.fulfill({ status: 503, json: { error: 'Could not save the change' } }), { times: 1 });
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/watch/history');
	const row = page.getByRole('listitem').filter({ has: page.getByRole('link', { name, exact: true }) });
	const remove = row.getByRole('button', { name: 'Remove', exact: true });
	const error = page.getByText(`Couldn't remove ${name}`);
	await expect(row).toBeVisible();
	await expect(row.getByRole('link', { name: 'Resume', exact: true })).toBeVisible();
	await remove.click();
	await expect(error).toBeVisible();
	await expect(row).toBeVisible();
	await noSidewaysScroll(page);
	await remove.click();
	await expect(row).toHaveCount(0);
	await expect(error).toBeHidden();
	const history = await page.request.get('/api/v1/stream/history');
	expect(history.ok()).toBe(true);
	const { items } = (await history.json()) as { items: { key: string }[] };
	expect(items.some((entry) => entry.key === key)).toBe(false);
});

for (const { name, season, nextEpisode } of [
	{ name: 'Continue Watching advances past a finished episode', season: 1, nextEpisode: 2 },
	{ name: 'Continue Watching opens the show if the next episode has yet to air', season: 2 }
]) {
	test(name, async ({ page }) => {
		test.setTimeout(90_000);
		await signIn(page, owner());
		const { csrf } = (await (await page.request.get('/api/v1/auth/me')).json()) as { csrf: string };
		const saved = await page.request.put('/api/v1/stream/history', {
			headers: { 'X-CSRF-Token': csrf },
			data: { kind: 'show', tmdb: '800001', title: 'Harbour Lights', season, episode: 1, position_sec: 2520, duration_sec: 2520, completed: true }
		});
		expect(saved.ok()).toBe(true);
		const { key } = (await saved.json()) as { key: string };
		try {
			await page.route('**/api/v1/stream/search', (route) => route.fulfill({ json: { candidates: [] } }));
			await page.goto('/', { waitUntil: 'domcontentloaded' });
			const play = page.getByTestId('continue-watching').locator('a[href*="/watch/show/800001"]').first();
			await expect(play).toBeVisible({ timeout: 45_000 });
			if (nextEpisode) {
				const lookup = page.waitForRequest((r) => new URL(r.url()).pathname.endsWith('/api/v1/stream/search') && r.method() === 'POST');
				await play.click();
				expect((await lookup).postDataJSON()).toMatchObject({ season, episode: nextEpisode });
				await expect(page).toHaveURL(/\/watch\/show\/800001\?s=1&e=2&resume=1/);
			} else {
				await page.evaluate(() => {
					const root = document.documentElement;
					root.dataset.continuePlayer = 'clear';
					const check = () => {
						if (root.dataset.continuePlayer !== 'clear') return;
						if (document.querySelector('.wp-stage')) root.dataset.continuePlayer = 'seen';
						requestAnimationFrame(check);
					};
					requestAnimationFrame(check);
				});
				await play.click();
				await expect(page).toHaveURL(/\/title\/show\/800001$/);
				await expect(page.getByRole('heading', { name: 'Harbour Lights', exact: true }).first()).toBeVisible();
				const observed = await page.locator('html').evaluate((root) => {
					const result = root.dataset.continuePlayer;
					delete root.dataset.continuePlayer;
					return result;
				});
				expect(observed, 'no player frame before showing the title').toBe('clear');
			}
		} finally {
			await page.request.delete(`/api/v1/stream/history/${encodeURIComponent(key)}`, { headers: { 'X-CSRF-Token': csrf } });
		}
	});
}
