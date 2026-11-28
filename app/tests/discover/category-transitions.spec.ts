import { expect, test } from '@playwright/test';
import { owner, signIn } from '../helpers/helpers.ts';

function category(id: string, name: string) {
	return { id, name, section: 'genres', section_title: 'Genres', movie: true, show: true };
}

function item(name: string, id: string) {
	return { kind: 'movie', name, tmdb_id: id, year: 2023 };
}

const legal = category('legal', 'Legal');
const courtroom = category('courtroom', 'Courtroom');
const medical = category('medical', 'Medical');
const legalItem = item('Legal fixture', '900002');
const medicalItem = item('Medical fixture', '900003');

test.beforeEach(async ({ page }) => {
	await signIn(page, owner());
});

test('a slow and failed category preview keeps the current shelf and selection', async ({ page }) => {
	await page.route('**/api/v1/search?**', (r) => r.fulfill({
		json: {
			query: { text: 'legal', plain: true },
			titles: [], more: [],
			categories: [legal, courtroom],
			category_id: legal.id, category_name: legal.name, category_items: [legalItem]
		}
	}));
	let release!: () => void;
	const pending = new Promise<void>((resolve) => { release = resolve; });
	await page.route('**/api/v1/explore/browse/category?**', async (r) => {
		await pending;
		await r.fulfill({ status: 503, json: { error: 'fixture unavailable' } });
	});
	await page.goto('/search?q=legal');
	const matches = page.getByTestId('search-category-matches');
	const shelf = matches.getByRole('heading', { name: 'Legal picks' });
	const chip = matches.getByRole('button', { name: 'Courtroom', exact: true });
	await expect(shelf).toBeVisible();
	try {
		await chip.click();
		await expect(chip).toHaveAttribute('aria-busy', 'true');
		await expect(shelf).toBeVisible();
		await expect(matches.getByText(legalItem.name, { exact: true })).toBeVisible();
	} finally {
		release();
	}
	await expect(matches.getByText('Could not load Courtroom. Try again.')).toBeVisible();
	await expect(matches.getByRole('button', { name: 'Legal', exact: true })).toHaveAttribute('aria-pressed', 'true');
	await expect(shelf).toBeVisible();
});

test('Explore labels the newly picked category during loading', async ({ page }) => {
	await page.route('**/api/v1/explore/subcategories?**', (r) => r.fulfill({
		json: { items: [legal, medical] }
	}));
	let release!: () => void;
	const pending = new Promise<void>((resolve) => { release = resolve; });
	await page.route('**/api/v1/explore/browse/category?**', async (r) => {
		const id = new URL(r.request().url()).searchParams.get('id');
		if (id === medical.id) {
			await pending;
			await r.fulfill({ json: { ...medical, items: [medicalItem] } });
		} else {
			await r.fulfill({ json: { ...legal, items: [legalItem] } });
		}
	});
	await page.goto('/explore?cat=legal');
	const categoryView = page.getByTestId('explore-subcategory-view');
	await expect(categoryView.getByText(legalItem.name, { exact: true })).toBeVisible();
	try {
		await page.getByTestId('explore-subcat-bar').getByRole('button', { name: 'Medical', exact: true }).click();
		await expect(categoryView.getByRole('heading', { name: 'Medical', exact: true })).toBeVisible();
		await expect(categoryView.getByRole('heading', { name: 'Legal', exact: true })).toHaveCount(0);
	} finally {
		release();
	}
	await expect(categoryView.getByText(medicalItem.name, { exact: true })).toBeVisible();
});
