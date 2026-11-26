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
	const reqObj = input && typeof input === 'object' ? input : null;
	let targetUrl = typeof input === 'string' ? input : reqObj?.url;
	await assertPublicURL(targetUrl);
	const ac = new AbortController();
	const upstreamSignal = init.signal || reqObj?.signal;
	if (upstreamSignal) {
		if (upstreamSignal.aborted) ac.abort(upstreamSignal.reason);
		else upstreamSignal.addEventListener('abort', () => ac.abort(upstreamSignal.reason), { once: true });
	}
	const timer = setTimeout(() => ac.abort(new Error('Fetch timeout')), 8000);
	try {
		const opts = {
			method: init.method || reqObj?.method || 'GET',
			headers: init.headers || reqObj?.headers || {},
			redirect: 'manual',
			signal: ac.signal
		};
		if (init.body !== undefined && opts.method !== 'GET' && opts.method !== 'HEAD') {
			opts.body = init.body;
		}
		let resp = await fetch(targetUrl, opts);
		let hops = 0;
		const follow = init.redirect !== 'manual';
		while (follow && resp.status >= 300 && resp.status < 400 && resp.headers.get('location') && hops < 6) {
			hops++;
			targetUrl = new URL(resp.headers.get('location'), targetUrl).toString();
			await assertPublicURL(targetUrl);
			const nextMethod = (resp.status === 307 || resp.status === 308) ? opts.method : 'GET';
			const nextBody = nextMethod === opts.method ? opts.body : undefined;
			resp = await fetch(targetUrl, { ...opts, method: nextMethod, body: nextBody });
		}
		const buf = Buffer.from(await resp.arrayBuffer());
		if (buf.byteLength > 4 * 1024 * 1024) {
			throw new Error('Response exceeded 4 MB limit');
		}
		const textVal = buf.toString('utf8');
		const ab = buf.buffer.slice(buf.byteOffset, buf.byteOffset + buf.byteLength);
		return {
			ok: resp.ok,
			status: resp.status,
			statusText: resp.statusText,
			url: resp.url || targetUrl,
			redirected: hops > 0,
			headers: resp.headers,
			async text() { return textVal; },
			async json() { return JSON.parse(textVal); },
			async arrayBuffer() { return ab; },
			clone() { return this; }
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
		.replace(/&nbsp;/g, ' ')
		.replace(/&#x([0-9a-fA-F]+);/g, (_, h) => String.fromCharCode(parseInt(h, 16)))
		.replace(/&#(\d+);/g, (_, d) => String.fromCharCode(Number(d)));
}

function escapeAttr(val) {
	return String(val ?? '').replace(/&/g, '&amp;').replace(/"/g, '&quot;');
}

const VOID_TAGS = new Set(['area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'param', 'source', 'track', 'wbr']);

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

function makeEl(tag, attrs, parent) {
	return { tag, name: tag, tagName: tag, attrs, attribs: attrs, children: [], text: '', parent };
}

function parseHTML(html) {
	const root = makeEl('#root', {}, null);
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
			const node = makeEl(tag, parseAttrs(m[3] || ''), top);
			top.children.push(node);
			if (!m[4] && !VOID_TAGS.has(tag)) stack.push(node);
		} else if (m[5]) {
			const inRaw = top.tag === 'script' || top.tag === 'style';
			top.children.push({ tag: '#text', children: [], text: inRaw ? m[5] : decodeHtmlEntities(m[5]), parent: top });
		}
	}
	return root;
}

function nodeText(node) {
	if (!node) return '';
	if (node.tag === '#text') return node.text;
	return (node.children || []).map(nodeText).join('');
}

function serializeNode(node) {
	if (!node) return '';
	if (node.tag === '#text') return node.text;
	if (node.tag === '#root') return (node.children || []).map(serializeNode).join('');
	const attrStr = Object.entries(node.attrs || {})
		.map(([k, v]) => (v === '' ? ` ${k}` : ` ${k}="${escapeAttr(v)}"`))
		.join('');
	if (VOID_TAGS.has(node.tag)) return `<${node.tag}${attrStr}>`;
	return `<${node.tag}${attrStr}>${(node.children || []).map(serializeNode).join('')}</${node.tag}>`;
}

function elemChildren(node) {
	return (node?.children || []).filter((c) => c.tag && !c.tag.startsWith('#'));
}

function splitTopLevel(input, byComma) {
	const items = [];
	let cur = '';
	let quote = '';
	let sqDepth = 0;
	let parDepth = 0;
	for (let i = 0; i < input.length; i++) {
		const ch = input[i];
		if (quote) {
			cur += ch;
			if (ch === quote && input[i - 1] !== '\\') quote = '';
			continue;
		}
		if (ch === '"' || ch === "'") {
			quote = ch;
			cur += ch;
		} else if (ch === '[') {
			sqDepth++;
			cur += ch;
		} else if (ch === ']') {
			sqDepth = Math.max(0, sqDepth - 1);
			cur += ch;
		} else if (ch === '(') {
			parDepth++;
			cur += ch;
		} else if (ch === ')') {
			parDepth = Math.max(0, parDepth - 1);
			cur += ch;
		} else if (byComma && ch === ',' && sqDepth === 0 && parDepth === 0) {
			if (cur.trim()) items.push(cur.trim());
			cur = '';
		} else {
			cur += ch;
		}
	}
	if (cur.trim()) items.push(cur.trim());
	return items;
}

function tokenizeChain(group) {
	const tokens = [];
	let comb = ' ';
	let cur = '';
	let quote = '';
	let sq = 0;
	let par = 0;
	const flush = () => {
		if (cur.trim()) {
			tokens.push({ comb, sel: cur.trim() });
			comb = ' ';
			cur = '';
		}
	};
	for (let i = 0; i < group.length; i++) {
		const ch = group[i];
		if (quote) {
			cur += ch;
			if (ch === quote && group[i - 1] !== '\\') quote = '';
			continue;
		}
		if (ch === '"' || ch === "'") {
			quote = ch;
			cur += ch;
		} else if (ch === '[') {
			sq++;
			cur += ch;
		} else if (ch === ']') {
			sq = Math.max(0, sq - 1);
			cur += ch;
		} else if (ch === '(') {
			par++;
			cur += ch;
		} else if (ch === ')') {
			par = Math.max(0, par - 1);
			cur += ch;
		} else if (sq === 0 && par === 0 && (ch === '>' || ch === '+' || ch === '~')) {
			flush();
			comb = ch;
		} else if (sq === 0 && par === 0 && /\s/.test(ch)) {
			if (cur.trim()) flush();
		} else {
			cur += ch;
		}
	}
	flush();
	return tokens;
}

function stripPseudos(sel) {
	const pseudos = [];
	let base = '';
	let sq = 0;
	for (let i = 0; i < sel.length; i++) {
		const ch = sel[i];
		if (ch === '[') sq++;
		else if (ch === ']') sq = Math.max(0, sq - 1);
		if (ch === ':' && sq === 0) {
			let j = i + 1;
			while (j < sel.length && /[a-zA-Z0-9_-]/.test(sel[j])) j++;
			const name = sel.slice(i + 1, j).toLowerCase();
			let arg = '';
			if (sel[j] === '(') {
				let depth = 1;
				let k = j + 1;
				let q = '';
				while (k < sel.length && depth > 0) {
					if (q) {
						if (sel[k] === q && sel[k - 1] !== '\\') q = '';
					} else if (sel[k] === '"' || sel[k] === "'") q = sel[k];
					else if (sel[k] === '(') depth++;
					else if (sel[k] === ')') depth--;
					k++;
				}
				arg = sel.slice(j + 1, k - 1).trim();
				j = k;
			}
			pseudos.push({ name, arg });
			i = j - 1;
		} else {
			base += ch;
		}
	}
	return { base, pseudos };
}

function matchesNth(expr, oneBasedIndex) {
	const s = expr.replace(/\s+/g, '').toLowerCase();
	if (s === 'odd') return oneBasedIndex % 2 === 1;
	if (s === 'even') return oneBasedIndex % 2 === 0;
	if (/^\d+$/.test(s)) return oneBasedIndex === Number(s);
	const m = /^([+-]?\d*)n([+-]\d+)?$/.exec(s);
	if (!m) return false;
	const a = m[1] === '' || m[1] === '+' ? 1 : (m[1] === '-' ? -1 : Number(m[1]));
	const b = m[2] ? Number(m[2]) : 0;
	if (a === 0) return oneBasedIndex === b;
	const k = (oneBasedIndex - b) / a;
	return Number.isInteger(k) && k >= 0;
}

function matchesSimple(node, rawSel) {
	if (!node || !node.tag || node.tag.startsWith('#')) return false;
	const { base, pseudos } = stripPseudos(rawSel);
	const cleaned = base.replace(/\[[^\]]*\]/g, '');
	const tagMatch = cleaned.match(/^[a-zA-Z0-9_-]+|^\*/);
	if (tagMatch && tagMatch[0] !== '*') {
		if (node.tag !== tagMatch[0].toLowerCase()) return false;
	}
	for (const idm of cleaned.matchAll(/#([a-zA-Z0-9_-]+)/g)) {
		if ((node.attrs.id || '') !== idm[1]) return false;
	}
	for (const clm of cleaned.matchAll(/\.([a-zA-Z0-9_-]+)/g)) {
		const cls = String(node.attrs.class || '').split(/\s+/);
		if (!cls.includes(clm[1])) return false;
	}
	const attrRe = /\[([a-zA-Z0-9_:-]+)\s*(?:([*^$~|]?=)\s*(?:"([^"]*)"|'([^']*)'|([^\]]*)))?\]/g;
	for (const atm of base.matchAll(attrRe)) {
		const k = atm[1].toLowerCase();
		if (!(k in node.attrs)) return false;
		const op = atm[2];
		if (!op) continue;
		const actual = String(node.attrs[k] ?? '');
		const want = atm[3] ?? atm[4] ?? (atm[5] || '').trim();
		if (op === '=' && actual !== want) return false;
		if (op === '*=' && (!want || !actual.includes(want))) return false;
		if (op === '^=' && (!want || !actual.startsWith(want))) return false;
		if (op === '$=' && (!want || !actual.endsWith(want))) return false;
		if (op === '~=' && !actual.split(/\s+/).includes(want)) return false;
		if (op === '|=' && actual !== want && !actual.startsWith(want + '-')) return false;
	}
	for (const p of pseudos) {
		const sibs = node.parent ? elemChildren(node.parent) : [node];
		const idx = sibs.indexOf(node) + 1;
		if (p.name === 'first-child' && idx !== 1) return false;
		if (p.name === 'last-child' && idx !== sibs.length) return false;
		if (p.name === 'only-child' && sibs.length !== 1) return false;
		if (p.name === 'nth-child' && !matchesNth(p.arg, idx)) return false;
		if (p.name === 'contains') {
			const needle = p.arg.replace(/^["']|["']$/g, '');
			if (!nodeText(node).includes(needle)) return false;
		}
		if (p.name === 'not' && p.arg) {
			if (splitTopLevel(p.arg, true).some((sub) => matchesSimple(node, sub))) return false;
		}
	}
	return true;
}

function collectDescendants(roots, part) {
	const out = [];
	const seen = new Set();
	const walk = (n) => {
		for (const ch of elemChildren(n)) {
			if (matchesSimple(ch, part) && !seen.has(ch)) {
				seen.add(ch);
				out.push(ch);
			}
			walk(ch);
		}
	};
	for (const r of roots) walk(r);
	return out;
}

function applyStep(cur, comb, part, filterPos = true) {
	const { base, pseudos } = stripPseudos(part);
	let posPseudo = null;
	const kept = [];
	for (const p of pseudos) {
		if (filterPos && (p.name === 'first' || p.name === 'last' || p.name === 'eq')) posPseudo = p;
		else kept.push(p.arg ? `:${p.name}(${p.arg})` : `:${p.name}`);
	}
	const cleanPart = base + kept.join('');
	let next = [];
	const seen = new Set();
	const push = (el) => {
		if (!seen.has(el) && matchesSimple(el, cleanPart)) {
			seen.add(el);
			next.push(el);
		}
	};
	if (comb === ' ') {
		next = collectDescendants(cur, cleanPart);
	} else if (comb === '>') {
		for (const r of cur) for (const ch of elemChildren(r)) push(ch);
	} else if (comb === '+') {
		for (const r of cur) {
			const sibs = r.parent ? elemChildren(r.parent) : [];
			const idx = sibs.indexOf(r);
			if (idx >= 0 && idx + 1 < sibs.length) push(sibs[idx + 1]);
		}
	} else if (comb === '~') {
		for (const r of cur) {
			const sibs = r.parent ? elemChildren(r.parent) : [];
			const idx = sibs.indexOf(r);
			for (let i = idx + 1; i < sibs.length; i++) push(sibs[i]);
		}
	}
	if (posPseudo) {
		if (posPseudo.name === 'first') return next.slice(0, 1);
		if (posPseudo.name === 'last') return next.length ? [next[next.length - 1]] : [];
		if (posPseudo.name === 'eq') {
			const n = Number(posPseudo.arg);
			const k = n < 0 ? next.length + n : n;
			return next[k] ? [next[k]] : [];
		}
	}
	return next;
}

function queryAll(roots, selector) {
	const groups = splitTopLevel(String(selector || ''), true);
	const out = [];
	const seen = new Set();
	for (const group of groups) {
		const chain = tokenizeChain(group);
		if (chain.length === 0) continue;
		let cur = applyStep(roots, ' ', chain[0].sel);
		for (let i = 1; i < chain.length; i++) {
			cur = applyStep(cur, chain[i].comb, chain[i].sel);
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

function nodeMatchesSelector(node, selector, docRoot) {
	if (!selector || !node || !node.tag || node.tag.startsWith('#')) return false;
	const groups = splitTopLevel(String(selector), true);
	for (const group of groups) {
		const chain = tokenizeChain(group);
		if (chain.length === 1) {
			if (matchesSimple(node, chain[0].sel)) return true;
			continue;
		}
		if (docRoot && queryAll([docRoot], group).includes(node)) return true;
	}
	return false;
}

function camelDataKey(k) {
	return k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase());
}

function parseDataVal(v) {
	if (v === 'true') return true;
	if (v === 'false') return false;
	if (/^-?\d+(\.\d+)?$/.test(v)) return Number(v);
	if ((v.startsWith('{') && v.endsWith('}')) || (v.startsWith('[') && v.endsWith(']'))) {
		try { return JSON.parse(v); } catch {}
	}
	return v;
}

function createCheerioShim(html) {
	const doc = parseHTML(html);
	function wrap(nodes, prevObject = null) {
		const arr = [];
		const seen = new Set();
		for (const n of nodes) {
			if (n && !seen.has(n)) {
				seen.add(n);
				arr.push(n);
			}
		}
		const wrapChild = (list) => wrap(list, api);
		const api = {
			_isCheerio: true,
			_nodes: arr,
			length: arr.length,
			prevObject,
			find(sel) { return wrapChild(queryAll(arr, sel)); },
			first() { return wrapChild(arr.slice(0, 1)); },
			last() { return wrapChild(arr.length ? [arr[arr.length - 1]] : []); },
			eq(idx) {
				const k = idx < 0 ? arr.length + idx : idx;
				return wrapChild(arr[k] ? [arr[k]] : []);
			},
			slice(start, end) { return wrapChild(arr.slice(start, end)); },
			get(idx) {
				if (idx === undefined) return [...arr];
				return arr[idx < 0 ? arr.length + idx : idx];
			},
			filter(fnOrSel) {
				if (typeof fnOrSel === 'function') {
					return wrapChild(arr.filter((n, i) => fnOrSel.call(n, i, n)));
				}
				return wrapChild(arr.filter((n) => nodeMatchesSelector(n, fnOrSel, doc)));
			},
			not(fnOrSel) {
				if (typeof fnOrSel === 'function') {
					return wrapChild(arr.filter((n, i) => !fnOrSel.call(n, i, n)));
				}
				return wrapChild(arr.filter((n) => !nodeMatchesSelector(n, fnOrSel, doc)));
			},
			is(fnOrSel) {
				if (typeof fnOrSel === 'function') return arr.some((n, i) => Boolean(fnOrSel.call(n, i, n)));
				return arr.some((n) => nodeMatchesSelector(n, fnOrSel, doc));
			},
			hasClass(cls) {
				return arr.some((n) => String(n.attrs?.class || '').split(/\s+/).includes(cls));
			},
			children(sel) {
				const kids = arr.flatMap((n) => elemChildren(n));
				return wrapChild(sel ? kids.filter((k) => nodeMatchesSelector(k, sel, doc)) : kids);
			},
			contents() { return wrapChild(arr.flatMap((n) => n.children || [])); },
			parent(sel) {
				const ps = arr.map((n) => n.parent).filter((p) => p && p.tag !== '#root');
				return wrapChild(sel ? ps.filter((p) => nodeMatchesSelector(p, sel, doc)) : ps);
			},
			parents(sel) {
				const ps = [];
				for (const n of arr) {
					let cur = n.parent;
					while (cur && cur.tag !== '#root') {
						if (!sel || nodeMatchesSelector(cur, sel, doc)) ps.push(cur);
						cur = cur.parent;
					}
				}
				return wrapChild(ps);
			},
			closest(sel) {
				const out = [];
				for (const n of arr) {
					let cur = n;
					while (cur && cur.tag !== '#root') {
						if (nodeMatchesSelector(cur, sel, doc)) {
							out.push(cur);
							break;
						}
						cur = cur.parent;
					}
				}
				return wrapChild(out);
			},
			next(sel) {
				const out = [];
				for (const n of arr) {
					const sibs = n.parent ? elemChildren(n.parent) : [];
					const nx = sibs[sibs.indexOf(n) + 1];
					if (nx && (!sel || nodeMatchesSelector(nx, sel, doc))) out.push(nx);
				}
				return wrapChild(out);
			},
			nextAll(sel) {
				const out = [];
				for (const n of arr) {
					const sibs = n.parent ? elemChildren(n.parent) : [];
					for (let i = sibs.indexOf(n) + 1; i < sibs.length; i++) {
						if (!sel || nodeMatchesSelector(sibs[i], sel, doc)) out.push(sibs[i]);
					}
				}
				return wrapChild(out);
			},
			prev(sel) {
				const out = [];
				for (const n of arr) {
					const sibs = n.parent ? elemChildren(n.parent) : [];
					const pv = sibs[sibs.indexOf(n) - 1];
					if (pv && (!sel || nodeMatchesSelector(pv, sel, doc))) out.push(pv);
				}
				return wrapChild(out);
			},
			prevAll(sel) {
				const out = [];
				for (const n of arr) {
					const sibs = n.parent ? elemChildren(n.parent) : [];
					for (let i = sibs.indexOf(n) - 1; i >= 0; i--) {
						if (!sel || nodeMatchesSelector(sibs[i], sel, doc)) out.push(sibs[i]);
					}
				}
				return wrapChild(out);
			},
			siblings(sel) {
				const out = [];
				for (const n of arr) {
					const sibs = n.parent ? elemChildren(n.parent) : [];
					for (const s of sibs) {
						if (s !== n && (!sel || nodeMatchesSelector(s, sel, doc))) out.push(s);
					}
				}
				return wrapChild(out);
			},
			each(fn) {
				for (let i = 0; i < arr.length; i++) {
					if (fn.call(arr[i], i, arr[i]) === false) break;
				}
				return api;
			},
			map(fn) {
				const mapped = [];
				for (let i = 0; i < arr.length; i++) {
					const res = fn.call(arr[i], i, arr[i]);
					if (Array.isArray(res)) {
						for (const item of res) if (item != null) mapped.push(item);
					} else if (res != null) {
						mapped.push(res);
					}
				}
				const col = {
					length: mapped.length,
					get: (idx) => (idx === undefined ? mapped : mapped[idx < 0 ? mapped.length + idx : idx]),
					toArray: () => [...mapped],
					filter: (fn2) => mapped.filter(fn2),
					first: () => mapped[0]
				};
				return col;
			},
			attr(name, val) {
				if (arguments.length >= 2) {
					const k = String(name).toLowerCase();
					for (const n of arr) {
						if (n.attrs) n.attrs[k] = String(val ?? '');
					}
					return api;
				}
				if (typeof name === 'string') return arr[0]?.attrs?.[name.toLowerCase()];
				return arr[0]?.attrs || {};
			},
			data(name) {
				const attrs = arr[0]?.attrs;
				if (!attrs) return undefined;
				if (name !== undefined) {
					const rawKey = 'data-' + String(name).replace(/([A-Z])/g, '-$1').toLowerCase();
					return rawKey in attrs ? parseDataVal(attrs[rawKey]) : undefined;
				}
				const obj = {};
				for (const [k, v] of Object.entries(attrs)) {
					if (k.startsWith('data-')) obj[camelDataKey(k)] = parseDataVal(v);
				}
				return obj;
			},
			prop(name) {
				const n = arr[0];
				if (!n) return undefined;
				const k = String(name);
				if (k === 'tagName' || k === 'nodeName') return n.tag.toUpperCase();
				if (k === 'outerHTML') return serializeNode(n);
				if (k === 'innerHTML') return (n.children || []).map(serializeNode).join('');
				if (k === 'textContent') return nodeText(n);
				return n.attrs?.[k.toLowerCase()];
			},
			val() { return arr[0]?.attrs?.value ?? (arr[0] ? nodeText(arr[0]) : undefined); },
			text() { return arr.map(nodeText).join('').trim(); },
			html() { return arr[0] ? (arr[0].children || []).map(serializeNode).join('') : null; },
			remove() {
				for (const n of arr) {
					if (n.parent?.children) {
						n.parent.children = n.parent.children.filter((c) => c !== n);
					}
				}
				return api;
			},
			clone() { return wrapChild(arr.map((n) => parseHTML(serializeNode(n)).children[0]).filter(Boolean)); },
			end() { return prevObject || wrap([]); },
			add(sel) {
				const extra = typeof sel === 'string' ? queryAll([doc], sel) : (sel?._nodes || [sel]);
				return wrapChild([...arr, ...extra]);
			},
			toArray() { return [...arr]; },
			[Symbol.iterator]: function* () {
				for (const n of arr) yield n;
			}
		};
		for (let i = 0; i < arr.length; i++) api[i] = arr[i];
		return api;
	}
	const $ = (sel, context) => {
		if (!sel) return wrap([]);
		if (typeof sel === 'object') {
			if (sel._isCheerio) return sel;
			if (Array.isArray(sel)) return wrap(sel);
			if (sel.tag) return wrap([sel]);
			if (sel._nodes) return wrap(sel._nodes);
			return wrap([]);
		}
		const trimmed = String(sel).trim();
		if (trimmed.startsWith('<')) return wrap(elemChildren(parseHTML(trimmed)));
		if (context) {
			const ctxNodes = typeof context === 'string' ? queryAll([doc], context) : (context._nodes || [context.tag ? context : doc]);
			return wrap(queryAll(ctxNodes, trimmed));
		}
		return wrap(queryAll([doc], trimmed));
	};
	$.load = createCheerioShim;
	$.html = (sel) => (sel ? serializeNode(sel._nodes ? sel._nodes[0] : sel) : serializeNode(doc));
	return $;
}

function toWords(buf) {
	const words = [];
	for (let i = 0; i < buf.length; i += 4) {
		const w = ((buf[i] || 0) << 24) | ((buf[i + 1] || 0) << 16) | ((buf[i + 2] || 0) << 8) | (buf[i + 3] || 0);
		words.push(w);
	}
	return words;
}

function wordsToBuf(words, sigBytes) {
	const arr = Array.isArray(words) ? words : [];
	const len = typeof sigBytes === 'number' ? Math.max(0, sigBytes) : arr.length * 4;
	const buf = Buffer.alloc(len);
	for (let i = 0; i < len; i++) {
		const w = arr[i >>> 2] || 0;
		buf[i] = (w >>> (24 - (i % 4) * 8)) & 0xff;
	}
	return buf;
}

function toBuf(v) {
	if (Buffer.isBuffer(v)) return v;
	if (v instanceof Uint8Array) return Buffer.from(v);
	if (v && typeof v === 'object') {
		if (Buffer.isBuffer(v._buf)) return v._buf;
		if (v.ciphertext) return toBuf(v.ciphertext);
		if (Array.isArray(v.words)) return wordsToBuf(v.words, v.sigBytes);
	}
	return Buffer.from(String(v ?? ''), 'utf8');
}

function createWordObj(buf) {
	const b = Buffer.isBuffer(buf) ? buf : Buffer.from(buf);
	return {
		_buf: b,
		words: toWords(b),
		sigBytes: b.length,
		concat(other) { return createWordObj(Buffer.concat([b, toBuf(other)])); },
		clamp() {},
		toString(enc) {
			if (enc === CryptoJS.enc.Base64) return b.toString('base64');
			if (enc === CryptoJS.enc.Utf8) return b.toString('utf8');
			if (enc === CryptoJS.enc.Latin1) return b.toString('latin1');
			return b.toString('hex');
		}
	};
}

function evpBytesToKey(passBuf, saltBuf, keyLen, ivLen) {
	const chunks = [];
	let prev = Buffer.alloc(0);
	while (Buffer.concat(chunks).length < keyLen + ivLen) {
		prev = crypto.createHash('md5').update(Buffer.concat([prev, passBuf, saltBuf])).digest();
		chunks.push(prev);
	}
	const dk = Buffer.concat(chunks);
	return { key: dk.subarray(0, keyLen), iv: dk.subarray(keyLen, keyLen + ivLen) };
}

function fitKey(buf, allowed) {
	for (const len of allowed) {
		if (buf.length === len) return buf;
	}
	const target = allowed[allowed.length - 1];
	const out = Buffer.alloc(target);
	buf.copy(out, 0, 0, Math.min(buf.length, target));
	return out;
}

function applyZeroUnpad(buf) {
	let end = buf.length;
	while (end > 0 && buf[end - 1] === 0) end--;
	return buf.subarray(0, end);
}

function resolveAlg(family, keyLen, mode) {
	const m = typeof mode === 'string' ? mode : 'cbc';
	if (family === 'des-ede3') return m === 'ecb' ? 'des-ede3-ecb' : 'des-ede3-cbc';
	return `aes-${keyLen * 8}-${m}`;
}

function runCipherEncrypt(family, data, key, cfg = {}) {
	const plain = toBuf(data);
	const mode = typeof cfg?.mode === 'string' ? cfg.mode : 'cbc';
	const pad = cfg?.padding || 'pkcs7';
	if (typeof key === 'string' && !cfg?.iv) {
		const salt = crypto.randomBytes(8);
		const keyLen = family === 'des-ede3' ? 24 : 32;
		const ivLen = family === 'des-ede3' ? 8 : 16;
		const dk = evpBytesToKey(Buffer.from(key, 'utf8'), salt, keyLen, ivLen);
		const cipher = crypto.createCipheriv(resolveAlg(family, keyLen, 'cbc'), dk.key, dk.iv);
		const out = Buffer.concat([Buffer.from('Salted__'), salt, cipher.update(plain), cipher.final()]);
		const cp = { ciphertext: createWordObj(out), salt: createWordObj(salt), toString: () => out.toString('base64') };
		return cp;
	}
	const rawKey = fitKey(toBuf(key), family === 'des-ede3' ? [24] : [16, 24, 32]);
	const block = family === 'des-ede3' ? 8 : 16;
	const iv = mode === 'ecb' ? null : (cfg?.iv ? toBuf(cfg.iv).subarray(0, block) : Buffer.alloc(block));
	const cipher = crypto.createCipheriv(resolveAlg(family, rawKey.length, mode), rawKey, iv);
	if (pad !== 'pkcs7') cipher.setAutoPadding(false);
	let input = plain;
	if (pad === 'zero' && plain.length % block !== 0) {
		const rem = block - (plain.length % block);
		input = Buffer.concat([plain, Buffer.alloc(rem)]);
	}
	const out = Buffer.concat([cipher.update(input), cipher.final()]);
	return {
		ciphertext: createWordObj(out),
		iv: iv ? createWordObj(iv) : undefined,
		toString(enc) { return enc === CryptoJS.enc.Hex ? out.toString('hex') : out.toString('base64'); }
	};
}

function runCipherDecrypt(family, cipherInput, key, cfg = {}) {
	const raw = typeof cipherInput === 'string' ? Buffer.from(cipherInput, 'base64') : toBuf(cipherInput);
	const mode = typeof cfg?.mode === 'string' ? cfg.mode : 'cbc';
	const pad = cfg?.padding || 'pkcs7';
	if (typeof key === 'string' && !cfg?.iv) {
		if (raw.subarray(0, 8).toString('utf8') !== 'Salted__') return createWordObj(Buffer.alloc(0));
		const keyLen = family === 'des-ede3' ? 24 : 32;
		const ivLen = family === 'des-ede3' ? 8 : 16;
		const dk = evpBytesToKey(Buffer.from(key, 'utf8'), raw.subarray(8, 16), keyLen, ivLen);
		const decipher = crypto.createDecipheriv(resolveAlg(family, keyLen, 'cbc'), dk.key, dk.iv);
		return createWordObj(Buffer.concat([decipher.update(raw.subarray(16)), decipher.final()]));
	}
	const rawKey = fitKey(toBuf(key), family === 'des-ede3' ? [24] : [16, 24, 32]);
	const block = family === 'des-ede3' ? 8 : 16;
	const iv = mode === 'ecb' ? null : (cfg?.iv ? toBuf(cfg.iv).subarray(0, block) : Buffer.alloc(block));
	const decipher = crypto.createDecipheriv(resolveAlg(family, rawKey.length, mode), rawKey, iv);
	if (pad !== 'pkcs7') decipher.setAutoPadding(false);
	let out = Buffer.concat([decipher.update(raw), decipher.final()]);
	if (pad === 'zero') out = applyZeroUnpad(out);
	return createWordObj(out);
}

const CryptoJS = {
	lib: {
		WordArray: {
			create(words, sigBytes) { return createWordObj(wordsToBuf(words, sigBytes)); },
			random(n) { return createWordObj(crypto.randomBytes(Number(n) || 16)); }
		}
	},
	enc: {
		Hex: { parse: (s) => createWordObj(Buffer.from(String(s), 'hex')), stringify: (w) => toBuf(w).toString('hex') },
		Base64: { parse: (s) => createWordObj(Buffer.from(String(s), 'base64')), stringify: (w) => toBuf(w).toString('base64') },
		Utf8: { parse: (s) => createWordObj(Buffer.from(String(s), 'utf8')), stringify: (w) => toBuf(w).toString('utf8') },
		Latin1: { parse: (s) => createWordObj(Buffer.from(String(s), 'latin1')), stringify: (w) => toBuf(w).toString('latin1') }
	},
	mode: { CBC: 'cbc', ECB: 'ecb', CTR: 'ctr', CFB: 'cfb', OFB: 'ofb' },
	pad: { Pkcs7: 'pkcs7', PKCS7: 'pkcs7', ZeroPadding: 'zero', NoPadding: 'nopadding' },
	MD5: (v) => createWordObj(crypto.createHash('md5').update(toBuf(v)).digest()),
	SHA1: (v) => createWordObj(crypto.createHash('sha1').update(toBuf(v)).digest()),
	SHA256: (v) => createWordObj(crypto.createHash('sha256').update(toBuf(v)).digest()),
	SHA512: (v) => createWordObj(crypto.createHash('sha512').update(toBuf(v)).digest()),
	HmacMD5: (v, key) => createWordObj(crypto.createHmac('md5', toBuf(key)).update(toBuf(v)).digest()),
	HmacSHA1: (v, key) => createWordObj(crypto.createHmac('sha1', toBuf(key)).update(toBuf(v)).digest()),
	HmacSHA256: (v, key) => createWordObj(crypto.createHmac('sha256', toBuf(key)).update(toBuf(v)).digest()),
	HmacSHA512: (v, key) => createWordObj(crypto.createHmac('sha512', toBuf(key)).update(toBuf(v)).digest()),
	AES: {
		encrypt(text, key, cfg) { return runCipherEncrypt('aes', text, key, cfg); },
		decrypt(cipher, key, cfg) { return runCipherDecrypt('aes', cipher, key, cfg); }
	},
	TripleDES: {
		encrypt(text, key, cfg) { return runCipherEncrypt('des-ede3', text, key, cfg); },
		decrypt(cipher, key, cfg) { return runCipherDecrypt('des-ede3', cipher, key, cfg); }
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
			if (mod === 'cheerio' || mod === 'cheerio-without-node-native') return cheerioMod;
			if (mod === 'crypto-js') return CryptoJS;
			throw new Error('Blocked require in Nuvio plugin sandbox: ' + mod);
		},
		fetch: guardedFetch,
		cheerio: cheerioMod,
		CryptoJS,
		Buffer,
		Uint8Array,
		ArrayBuffer,
		DataView,
		Headers,
		Request,
		Response,
		AbortController,
		AbortSignal,
		URL,
		URLSearchParams,
		TextEncoder,
		TextDecoder,
		atob: (s) => Buffer.from(String(s), 'base64').toString('binary'),
		btoa: (s) => Buffer.from(String(s), 'binary').toString('base64'),
		setTimeout,
		clearTimeout,
		setInterval,
		clearInterval,
		console: silentConsole,
		SCRAPER_ID: String(input.scraperId || ''),
		SCRAPER_SETTINGS: input.settings || {},
		TMDB_API_KEY: String(input.tmdbApiKey || '')
	};
	sandbox.globalThis = sandbox;
	sandbox.global = sandbox;
	sandbox.window = sandbox;
	sandbox.crypto = crypto.webcrypto;
	sandbox.process = { env: {} };

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
