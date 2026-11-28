export interface PlayerFont { id: string; family: string; bytes: ArrayBuffer }
const loaded = new Map<string, FontFace>();
const fontStore = 'fonts';

function fontDatabase(): Promise<IDBDatabase> {
	return new Promise((resolve, reject) => {
		const request = indexedDB.open('holyarrs-player-fonts', 1);
		request.onupgradeneeded = () => { request.result.createObjectStore(fontStore, { keyPath: 'id' }); };
		request.onsuccess = () => resolve(request.result);
		request.onerror = () => reject(request.error);
	});
}

async function fontRequest<T>(mode: IDBTransactionMode, run: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
	const db = await fontDatabase();
	try {
		return await new Promise<T>((resolve, reject) => {
			const tx = db.transaction(fontStore, mode);
			const request = run(tx.objectStore(fontStore));
			tx.oncomplete = () => resolve(request.result);
			tx.onerror = () => reject(tx.error);
			tx.onabort = () => reject(tx.error);
		});
	} finally { db.close(); }
}

async function register(font: PlayerFont): Promise<void> {
	if (loaded.has(font.id)) return;
	const face = await new FontFace(font.family, font.bytes).load();
	document.fonts.add(face);
	loaded.set(font.id, face);
}

export async function loadPlayerFonts(): Promise<PlayerFont[]> {
	const fonts = await fontRequest('readonly', (store) => store.getAll()) as PlayerFont[];
	const results = await Promise.allSettled(fonts.map(register));
	return fonts.filter((_, i) => results[i].status === 'fulfilled');
}

export async function addPlayerFont(file: File): Promise<PlayerFont> {
	if (!/\.(ttf|otf|woff2?)$/i.test(file.name) || file.size > 12 * 1024 * 1024) throw new Error('Choose a TTF, OTF, WOFF or WOFF2 font up to 12 MB.');
	const family = file.name.replace(/\.[^.]*$/, '').replace(/["\\]/g, '').trim();
	if (!family) throw new Error('Give this font a name before adding it.');
	const font = { id: crypto.randomUUID(), family, bytes: await file.arrayBuffer() };
	await register(font);
	try { await fontRequest('readwrite', (store) => store.put(font)); }
	catch (error) { unregister(font.id); throw error; }
	return font;
}

function unregister(id: string): void {
	const face = loaded.get(id);
	if (face) document.fonts.delete(face);
	loaded.delete(id);
}

export async function removePlayerFont(id: string): Promise<void> {
	await fontRequest('readwrite', (store) => store.delete(id));
	unregister(id);
}
