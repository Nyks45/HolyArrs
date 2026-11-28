import area from './smaa/area.b64?raw';
import search from './smaa/search.b64?raw';
import { smaaArea, smaaSearch } from './smaa-shaders';

export function smaaTexture(id: number) {
	const data = id === smaaArea ? area : search;
	const [width, height] = id === smaaArea ? [160, 560] : [64, 16];
	if (id !== smaaArea && id !== smaaSearch) throw new Error('Unknown SMAA lookup texture');
	return { width, height, pixels: Uint8Array.from(atob(data), (value) => value.charCodeAt(0)) };
}
