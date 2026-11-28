import { to } from '#lib/core/paths.ts';
import { fetchStreamSubtitle, uploadStreamSubtitle, type SubtitleTrack } from './stream-api';
import { parseWebVTT, type VTTCue } from './vtt';

export interface LoadedSubtitle {
	cues: VTTCue[];
	rawVtt: string;
	ass: string;
	lang: string;
	label: string;
	subFile: string;
}

export async function resolveEmbeddedSub(sessionId: string, token: string, idx: number, track?: Partial<SubtitleTrack>): Promise<LoadedSubtitle> {
	const href = to(`/api/v1/stream/${encodeURIComponent(sessionId)}/sub/${encodeURIComponent(token)}/${idx}`);
	const styled = /^(ass|ssa)$/i.test(track?.codec ?? '');
	const [res, rawAss] = await Promise.all([
		fetch(href, { credentials: 'same-origin' }),
		styled ? fetch(`${href}?format=ass`, { credentials: 'same-origin' }).then(async (r) => r.ok ? r.text() : '').catch(() => '') : ''
	]);
	if (!res.ok) throw new Error(await res.text());
	const rawVtt = await res.text();
	return {
		cues: parseWebVTT(rawVtt), rawVtt, ass: rawAss, lang: track?.language ?? '',
		label: [track?.language?.toUpperCase(), track?.title].filter(Boolean).join(' · ') || `Track ${idx + 1}`, subFile: ''
	};
}

export async function resolveSiteSub(id: string, lang: string, label: string, sessionId: string): Promise<LoadedSubtitle> {
	const res = await fetchStreamSubtitle(id, sessionId);
	const resolvedLang = res.language || lang;
	return { cues: parseWebVTT(res.vtt), ass: res.ass ?? '', rawVtt: res.vtt, lang: resolvedLang, label: label || resolvedLang.toUpperCase(), subFile: res.sub_file ?? '' };
}

export async function resolveUploadedSub(file: File, sessionId: string): Promise<LoadedSubtitle> {
	const res = await uploadStreamSubtitle(file.name, await file.text(), '', sessionId);
	return { cues: parseWebVTT(res.vtt), ass: res.ass ?? '', rawVtt: res.vtt, lang: res.language ?? '', label: file.name, subFile: res.sub_file ?? '' };
}
