import vm from 'node:vm';
import crypto from 'node:crypto';
import dns from 'node:dns/promises';
import net from 'node:net';

function readStdin() {
	return new Promise((resolve, reject) => {
		const chunks = [];
		process.stdin.on('data', (c) => chunks.push(c));
		process.stdin.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
		process.stdin.on('error', reject);
	});
}

function isPrivateIPv4(ip) {
	const parts = ip.split('.').map((n) => Number(n));
	if (parts.length !== 4 || parts.some((n) => Number.isNaN(n) || n < 0 || n > 255)) return true;
	const [a, b] = parts;
	if (a === 0 || a === 10 || a === 127) return true;
	if (a === 169 && b === 254) return true;
	if (a === 172 && b >= 16 && b <= 31) return true;
	if (a === 192 && b === 168) return true;
	if (a === 100 && b >= 64 && b <= 127) return true;
	if (a >= 224) return true;
	return false;
}

function isPrivateIP(ip) {
	if (!ip) return true;
	const clean = ip.replace(/^\[|\]$/g, '').split('%')[0].toLowerCase();
	if (clean === '::1' || clean === '::' || clean === 'localhost') return true;
	if (clean.startsWith('::ffff:')) {
		return isPrivateIPv4(clean.slice(7));
	}
	if (net.isIPv4(clean)) return isPrivateIPv4(clean);
	if (net.isIPv6(clean)) {
		if (clean.startsWith('fe80:') || clean.startsWith('fc') || clean.startsWith('fd') || clean.startsWith('ff')) {
			return true;
		}
		return false;
	}
	return true;
}

async function assertPublicURL(rawUrl) {
	let parsed;
	try {
		parsed = new URL(String(rawUrl));
	} catch {
		throw new Error('Invalid URL');
	}
	if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
		throw new Error('Unsupported protocol: ' + parsed.protocol);
	}
	const host = parsed.hostname.replace(/^\[|\]$/g, '').toLowerCase();
	if (!host || host === 'localhost' || host.endsWith('.local') || host.endsWith('.internal')) {
		throw new Error('Blocked private hostname: ' + host);
	}
	if (net.isIP(host)) {
		if (isPrivateIP(host)) throw new Error('Blocked private IP literal: ' + host);
		return parsed;
	}
	const addrs = await dns.lookup(host, { all: true });
	if (!addrs || addrs.length === 0) throw new Error('DNS resolution failed');
	for (const a of addrs) {
		if (isPrivateIP(a.address)) {
			throw new Error('Blocked resolved private IP: ' + a.address);
		}
	}
	return parsed;
}

async function guardedFetch(input, init = {}) {
	const targetUrl = typeof input === 'string' ? input : input?.url;
	await assertPublicURL(targetUrl);
	const ac = new AbortController();
	const timer = setTimeout(() => ac.abort(new Error('Fetch timeout')), 8000);
	try {
		const opts = {
			method: init.method || 'GET',
			headers: init.headers || {},
			redirect: 'manual',
			signal: ac.signal
		};
		if (init.body !== undefined && opts.method !== 'GET' && opts.method !== 'HEAD') {
			opts.body = init.body;
		}
		let resp = await fetch(targetUrl, opts);
		let hops = 0;
		while (resp.status >= 300 && resp.status < 400 && resp.headers.get('location') && hops < 4) {
			hops++;
			const nextUrl = new URL(resp.headers.get('location'), targetUrl).toString();
			await assertPublicURL(nextUrl);
			resp = await fetch(nextUrl, { ...opts, method: 'GET', body: undefined });
		}
		const buf = await resp.arrayBuffer();
		if (buf.byteLength > 2 * 1024 * 1024) {
			throw new Error('Response exceeded 2 MB limit');
		}
		const textVal = Buffer.from(buf).toString('utf8');
		return {
			ok: resp.ok,
			status: resp.status,
			statusText: resp.statusText,
			url: resp.url || targetUrl,
			headers: Object.fromEntries(resp.headers.entries()),
			async text() { return textVal; },
			async json() { return JSON.parse(textVal); }
		};
	} finally {
		clearTimeout(timer);
	}
}

function decodeHtmlEntities(str) {
	return String(str || '')
		.replace(/&amp;/g, '&')
		.replace(/&lt;/g, '<')
		.replace(/&gt;/g, '>')
		.replace(/&quot;/g, '"')
		.replace(/&#39;|&apos;/g, "'")
		.replace(/&#(\d+);/g, (_, d) => String.fromCharCode(Number(d)));
}

function parseAttrs(raw) {
	const attrs = {};
	const re = /([a-zA-Z0-9_:-]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>`]+)))?/g;
	let m;
	while ((m = re.exec(raw)) !== null) {
		const k = m[1].toLowerCase();
		const v = m[2] ?? m[3] ?? m[4] ?? '';
		attrs[k] = decodeHtmlEntities(v);
	}
	return attrs;
}

function parseHTML(html) {
	const voidTags = new Set(['area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'param', 'source', 'track', 'wbr']);
	const root = { tag: '#root', attrs: {}, children: [], text: '' };
	const stack = [root];
	const tokenRe = /<!--[\s\S]*?-->|<\/([a-zA-Z0-9_-]+)\s*>|<([a-zA-Z0-9_-]+)([^>]*?)(\/?)>|([^<]+)/g;
	let m;
	while ((m = tokenRe.exec(String(html || ''))) !== null) {
		const top = stack[stack.length - 1];
		if (m[1]) {
			const closeTag = m[1].toLowerCase();
			for (let i = stack.length - 1; i > 0; i--) {
				if (stack[i].tag === closeTag) {
					stack.length = i;
					break;
				}
			}
		} else if (m[2]) {
			const tag = m[2].toLowerCase();
			const node = { tag, attrs: parseAttrs(m[3] || ''), children: [], text: '' };
			top.children.push(node);
			if (!m[4] && !voidTags.has(tag)) stack.push(node);
		} else if (m[5]) {
			top.children.push({ tag: '#text', attrs: {}, children: [], text: decodeHtmlEntities(m[5]) });
		}
	}
	return root;
}

function nodeText(node) {
	if (!node) return '';
	if (node.tag === '#text') return node.text;
	return node.children.map(nodeText).join('');
}

function matchesSimple(node, sel) {
	if (!node || !node.tag || node.tag.startsWith('#')) return false;
	const parts = [];
	const tagMatch = sel.match(/^[a-zA-Z0-9_-]+/);
	if (tagMatch && tagMatch[0] !== '*') {
		if (node.tag !== tagMatch[0].toLowerCase()) return false;
	}
	for (const idm of sel.matchAll(/#([a-zA-Z0-9_-]+)/g)) {
		if ((node.attrs.id || '') !== idm[1]) return false;
	}
	for (const clm of sel.matchAll(/\.([a-zA-Z0-9_-]+)/g)) {
		const cls = String(node.attrs.class || '').split(/\s+/);
		if (!cls.includes(clm[1])) return false;
	}
	for (const atm of sel.matchAll(/\[([a-zA-Z0-9_:-]+)(?:=(?:"([^"]*)"|'([^']*)'|([^\]]*)))?\]/g)) {
		const k = atm[1].toLowerCase();
		if (!(k in node.attrs)) return false;
		const want = atm[2] ?? atm[3] ?? atm[4];
		if (want !== undefined && node.attrs[k] !== want) return false;
	}
	void parts;
	return true;
}

function queryAll(roots, selector) {
	const groups = String(selector || '').split(',').map((s) => s.trim()).filter(Boolean);
	const out = [];
	const seen = new Set();
	for (const group of groups) {
		const chain = group.split(/\s+/).filter(Boolean);
		let cur = roots;
		for (const part of chain) {
			const next = [];
			const walk = (n) => {
				for (const ch of n.children || []) {
					if (matchesSimple(ch, part)) next.push(ch);
					walk(ch);
				}
			};
			for (const r of cur) walk(r);
			cur = next;
		}
		for (const n of cur) {
			if (!seen.has(n)) {
				seen.add(n);
				out.push(n);
			}
		}
	}
	return out;
}

function createCheerioShim(html) {
	const doc = parseHTML(html);
	function wrap(nodes) {
		const arr = nodes.filter(Boolean);
		const api = {
			length: arr.length,
			find(sel) { return wrap(queryAll(arr, sel)); },
			first() { return wrap(arr.slice(0, 1)); },
			last() { return wrap(arr.length ? [arr[arr.length - 1]] : []); },
			eq(idx) { return wrap(arr[idx] ? [arr[idx]] : []); },
			each(fn) {
				arr.forEach((n, i) => fn.call(wrap([n]), i, wrap([n])));
				return api;
			},
			map(fn) {
				const mapped = arr.map((n, i) => fn.call(wrap([n]), i, wrap([n])));
				return { get: () => mapped, length: mapped.length };
			},
			attr(name) { return arr[0]?.attrs?.[String(name).toLowerCase()]; },
			text() { return arr.map(nodeText).join('').trim(); },
			html() { return nodeText(arr[0]); },
			toArray() { return arr.map((n) => wrap([n])); },
			[Symbol.iterator]: function* () {
				for (const n of arr) yield wrap([n]);
			}
		};
		return api;
	}
	const $ = (sel, context) => {
		if (!sel) return wrap([]);
		if (typeof sel === 'object') return sel.length !== undefined ? sel : wrap([sel]);
		if (String(sel).trim().startsWith('<')) return wrap(parseHTML(sel).children);
		if (context) return wrap(queryAll(context.toArray ? [parseHTML(context.html())] : [doc], sel));
		return wrap(queryAll([doc], sel));
	};
	$.load = createCheerioShim;
	return $;
}

function createWordObj(buf) {
	return {
		_buf: buf,
		toString(enc) {
			if (enc === CryptoJS.enc.Base64) return buf.toString('base64');
			if (enc === CryptoJS.enc.Utf8) return buf.toString('utf8');
			return buf.toString('hex');
		}
	};
}

const CryptoJS = {
	enc: {
		Hex: { parse: (s) => createWordObj(Buffer.from(String(s), 'hex')), stringify: (w) => w._buf.toString('hex') },
		Base64: { parse: (s) => createWordObj(Buffer.from(String(s), 'base64')), stringify: (w) => w._buf.toString('base64') },
		Utf8: { parse: (s) => createWordObj(Buffer.from(String(s), 'utf8')), stringify: (w) => w._buf.toString('utf8') },
	},
	MD5: (v) => createWordObj(crypto.createHash('md5').update(String(v)).digest()),
	SHA1: (v) => createWordObj(crypto.createHash('sha1').update(String(v)).digest()),
	SHA256: (v) => createWordObj(crypto.createHash('sha256').update(String(v)).digest()),
	HmacSHA256: (v, key) => createWordObj(crypto.createHmac('sha256', String(key)).update(String(v)).digest()),
	AES: {
		encrypt(text, key) {
			const salt = crypto.randomBytes(8);
			const pass = Buffer.from(String(key), 'utf8');
			const derived = [];
			let prev = Buffer.alloc(0);
			while (Buffer.concat(derived).length < 48) {
				prev = crypto.createHash('md5').update(Buffer.concat([prev, pass, salt])).digest();
				derived.push(prev);
			}
			const dk = Buffer.concat(derived);
			const cipher = crypto.createCipheriv('aes-256-cbc', dk.subarray(0, 32), dk.subarray(32, 48));
			const enc = Buffer.concat([Buffer.from('Salted__'), salt, cipher.update(String(text), 'utf8'), cipher.final()]);
			return { toString: () => enc.toString('base64') };
		},
		decrypt(cipherB64, key) {
			const raw = Buffer.from(String(cipherB64), 'base64');
			if (raw.subarray(0, 8).toString('utf8') !== 'Salted__') {
				return createWordObj(Buffer.alloc(0));
			}
			const salt = raw.subarray(8, 16);
			const data = raw.subarray(16);
			const pass = Buffer.from(String(key), 'utf8');
			const derived = [];
			let prev = Buffer.alloc(0);
			while (Buffer.concat(derived).length < 48) {
				prev = crypto.createHash('md5').update(Buffer.concat([prev, pass, salt])).digest();
				derived.push(prev);
			}
			const dk = Buffer.concat(derived);
			const decipher = crypto.createDecipheriv('aes-256-cbc', dk.subarray(0, 32), dk.subarray(32, 48));
			return createWordObj(Buffer.concat([decipher.update(data), decipher.final()]));
		}
	}
};

async function main() {
	const raw = await readStdin();
	const input = JSON.parse(raw || '{}');
	const code = String(input.code || '');
	if (!code) {
		process.stdout.write(JSON.stringify({ streams: [], error: 'empty scraper code' }));
		return;
	}

	let cheerioMod = { load: createCheerioShim };
	try {
		const realCheerio = await import('cheerio');
		if (realCheerio?.load) cheerioMod = realCheerio;
	} catch {
		/* Built-in pure JS cheerio shim used when external cheerio package is absent. */
	}

	const moduleObj = { exports: {} };
	const silentConsole = { log() {}, warn() {}, error() {}, info() {}, debug() {} };
	const sandbox = {
		module: moduleObj,
		exports: moduleObj.exports,
		require(mod) {
			if (mod === 'cheerio') return cheerioMod;
			if (mod === 'crypto-js') return CryptoJS;
			throw new Error('Blocked require in Nuvio plugin sandbox: ' + mod);
		},
		fetch: guardedFetch,
		cheerio: cheerioMod,
		CryptoJS,
		URL,
		URLSearchParams,
		TextEncoder,
		TextDecoder,
		atob: (s) => Buffer.from(String(s), 'base64').toString('binary'),
		btoa: (s) => Buffer.from(String(s), 'binary').toString('base64'),
		setTimeout,
		clearTimeout,
		console: silentConsole,
		SCRAPER_ID: String(input.scraperId || ''),
		SCRAPER_SETTINGS: input.settings || {},
		TMDB_API_KEY: String(input.tmdbApiKey || '')
	};
	sandbox.globalThis = sandbox;
	sandbox.window = sandbox;

	const ctx = vm.createContext(sandbox, { codeGeneration: { strings: false, wasm: false } });
	const wrapped = `${code}\n;(typeof getStreams === 'function' ? getStreams : (module.exports && (module.exports.getStreams || module.exports.default)) || null);`;
	const fn = vm.runInContext(wrapped, ctx, { timeout: 3000 });
	const targetFn = typeof fn === 'function' ? fn : (typeof sandbox.getStreams === 'function' ? sandbox.getStreams : null);
	if (!targetFn) {
		process.stdout.write(JSON.stringify({ streams: [], error: 'getStreams function not found' }));
		return;
	}
	const result = await targetFn(String(input.tmdbId || ''), String(input.mediaType || 'movie'), input.season || null, input.episode || null);
	const list = Array.isArray(result) ? result : (result && Array.isArray(result.streams) ? result.streams : []);
	const norm = list.filter((x) => x && typeof x === 'object' && typeof x.url === 'string' && x.url.trim()).map((x) => ({
		name: String(x.name || ''),
		title: String(x.title || x.name || ''),
		url: String(x.url).trim(),
		quality: String(x.quality || ''),
		size: Number(x.sizeBytes || x.size || 0) || 0,
		headers: (x.headers && typeof x.headers === 'object') ? x.headers : {}
	}));
	process.stdout.write(JSON.stringify({ streams: norm }));
}

main().catch((err) => {
	process.stdout.write(JSON.stringify({ streams: [], error: String(err?.message || err || 'runner error') }));
});
