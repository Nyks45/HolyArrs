import { message } from '#lib/core/api.ts';
import { runAutoFindAndSyncSubtitle, syncVttToSessionAudio } from './player-media';
import { searchStreamSubtitles, type SessionStatus, type StreamSubCandidate, type StreamSubSearchReq, type SubPref } from './stream-api';
import { resolveEmbeddedSub, resolveSiteSub, resolveUploadedSub, type LoadedSubtitle } from './subtitle-tracks';
import type { VTTCue } from './vtt';

interface Context {
	session: SessionStatus | null;
	title: Required<Omit<StreamSubSearchReq, 'languages'>>;
	preferred: string[];
	autoSync: boolean;
	position: number;
}

export function createPlayerSubtitles(context: () => Context, persist: () => void, feedback: (text: string) => void) {
	const state = $state({
		subMode: 'off' as SubPref['mode'], subIndex: 0, subLang: '', subLabel: '', subSiteId: '', subSiteKey: '', subFile: '',
		subDelayMS: 0, subSize: 'md' as NonNullable<SubPref['size']>, parsedCues: [] as VTTCue[],
		siteSubLangsInput: 'en, tr', siteSubCandidates: [] as StreamSubCandidate[], searchingSubs: false, loadingSub: false,
		syncingSub: false, activeSubVtt: '', syncNote: '', subError: '', rawAss: '', subHidden: false,
		secondary: null as LoadedSubtitle | null, target: 'primary' as 'primary' | 'secondary'
	});
	let requests = 0, searches = 0;
	const languages = () => state.siteSubLangsInput.split(',').map((lang) => lang.trim()).filter(Boolean);

	function current(request: number, ctx: Context): boolean {
		const now = context();
		return request === requests && ctx.session?.id === now.session?.id && ctx.title.season === now.title.season && ctx.title.episode === now.title.episode;
	}

	function apply(track: LoadedSubtitle, mode: SubPref['mode'], idx: number, id: string, delay = 0, note = '') {
		state.parsedCues = track.cues; state.activeSubVtt = track.rawVtt; state.rawAss = track.ass; state.subMode = mode;
		state.subLang = track.lang; state.subLabel = track.label; state.subFile = track.subFile;
		state.subIndex = idx; state.subSiteId = id; state.subDelayMS = delay; state.syncNote = note; state.subHidden = false;
	}

	function clear() {
		requests++; searches++;
		state.siteSubCandidates = []; state.searchingSubs = false;
		state.subMode = 'off'; state.subLabel = ''; state.subSiteId = ''; state.subFile = ''; state.activeSubVtt = '';
		state.rawAss = ''; state.subHidden = false; state.syncNote = ''; state.parsedCues = []; state.secondary = null;
		state.target = 'primary'; state.loadingSub = false; state.syncingSub = false;
	}

	async function load(ctx: Context, mode: SubPref['mode'], source: Promise<LoadedSubtitle>, options: { silent?: boolean; index?: number; id?: string } = {}) {
		const request = ++requests;
		const target = options.silent ? 'primary' : state.target;
		state.loadingSub = true; state.subError = '';
		try {
			const data = await source;
			if (!current(request, ctx)) return;
			if (target === 'secondary') { state.secondary = data; state.subHidden = false; }
			else apply(data, mode, options.index ?? 0, options.id ?? '');
			if (!options.silent) feedback(`${target === 'secondary' ? 'Second subtitle' : 'Subs'}: ${data.label}`);
			persist();
		} catch (error) { if (current(request, ctx)) state.subError = message(error); }
		finally { if (request === requests) state.loadingSub = false; }
	}

	function loadEmbeddedSubtitle(index: number, silent = false) {
		const ctx = context();
		if (!ctx.session) return;
		const { id, token, probe } = ctx.session;
		return load(ctx, 'embedded', resolveEmbeddedSub(id, token, index, probe?.subtitle_tracks.find((track) => track.index === index)), { index, silent });
	}

	function loadSiteSubtitleById(id: string, lang: string, label: string, silent: boolean) {
		const ctx = context();
		return load(ctx, 'site', resolveSiteSub(id, lang, label, state.target === 'secondary' && !silent ? '' : ctx.session?.id ?? ''), { id, silent });
	}

	function handleSubtitleFileUpload(event: Event) {
		const picker = event.currentTarget as HTMLInputElement;
		const file = picker.files?.[0];
		if (!file) return;
		picker.value = '';
		const ctx = context();
		return load(ctx, 'upload', resolveUploadedSub(file, state.target === 'secondary' ? '' : ctx.session?.id ?? ''));
	}

	async function searchSubtitlesOnline() {
		const ctx = context(), search = ++searches;
		state.searchingSubs = true; state.subError = '';
		try {
			const result = await searchStreamSubtitles({ ...ctx.title, languages: languages() });
			if (search === searches) state.siteSubCandidates = result.candidates ?? [];
		} catch (error) { if (search === searches) state.subError = message(error); }
		finally { if (search === searches) state.searchingSubs = false; }
	}

	async function autoFindAndSyncSubtitle(silent = false) {
		const ctx = context();
		if (!ctx.session || state.syncingSub) return;
		const request = ++requests;
		state.syncingSub = true; state.loadingSub = false; state.subError = '';
		if (!silent) state.syncNote = 'Finding & syncing subtitle…';
		try {
			const result = await runAutoFindAndSyncSubtitle({ ...ctx.title, sessionId: ctx.session.id, releaseName: ctx.title.release_name, preferredLangs: ctx.preferred.length ? ctx.preferred : languages(), startSec: ctx.position, existingCandidates: state.siteSubCandidates, autoSync: !silent || ctx.autoSync });
			if (!current(request, ctx)) return;
			state.siteSubCandidates = result.cands;
			apply(result, 'site', 0, result.best.id, result.delayMs, result.note);
			if (!silent) feedback(result.note);
			persist();
		} catch (error) { if (current(request, ctx)) { if (!silent) state.subError = message(error); state.syncNote = ''; } }
		finally { state.syncingSub = false; }
	}

	async function syncCurrentSubtitleToAudio() {
		const ctx = context();
		if (!ctx.session || !state.activeSubVtt || state.syncingSub) return;
		const request = ++requests;
		state.syncingSub = true; state.loadingSub = false; state.subError = ''; state.syncNote = 'Listening to audio…';
		try {
			const result = await syncVttToSessionAudio({ sessionId: ctx.session.id, rawVtt: state.activeSubVtt, startSec: ctx.position });
			if (!current(request, ctx)) return;
			state.subDelayMS = result.offsetMs; state.syncNote = result.note; feedback(result.note); persist();
		} catch (error) { if (current(request, ctx)) { state.subError = message(error); state.syncNote = ''; } }
		finally { state.syncingSub = false; }
	}

	return { state, clear, loadEmbeddedSubtitle, loadSiteSubtitleById, handleSubtitleFileUpload, searchSubtitlesOnline, autoFindAndSyncSubtitle, syncCurrentSubtitleToAudio,
		turnOffSubtitles: () => { clear(); persist(); } };
}
