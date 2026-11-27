export type EpisodeReleaseStatus = 'aired' | 'today' | 'upcoming' | 'tba';

export interface EpisodeReleaseInfo {
	status: EpisodeReleaseStatus;
	isReleased: boolean;
	isNew: boolean;
	daysUntil: number | null;
	badge: string;
}

const DAY_MS = 86_400_000;

/** Classifies a YYYY-MM-DD air date relative to local calendar today. */
export function classifyAirDate(airDay?: string, now = new Date()): EpisodeReleaseInfo {
	const raw = (airDay ?? '').trim().slice(0, 10);
	const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(raw);
	if (!m) {
		return { status: 'tba', isReleased: false, isNew: false, daysUntil: null, badge: 'TBA' };
	}
	const year = Number(m[1]);
	const month = Number(m[2]);
	const day = Number(m[3]);
	const epDate = new Date(Date.UTC(year, month - 1, day));
	if (
		Number.isNaN(epDate.getTime()) ||
		epDate.getUTCFullYear() !== year ||
		epDate.getUTCMonth() !== month - 1 ||
		epDate.getUTCDate() !== day
	) {
		return { status: 'tba', isReleased: false, isNew: false, daysUntil: null, badge: 'TBA' };
	}
	const todayUtc = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate());
	const diff = Math.round((epDate.getTime() - todayUtc) / DAY_MS);
	if (diff === 0) {
		return { status: 'today', isReleased: true, isNew: true, daysUntil: 0, badge: 'Airs today' };
	}
	if (diff === 1) {
		return { status: 'upcoming', isReleased: false, isNew: false, daysUntil: 1, badge: 'Tomorrow' };
	}
	if (diff > 1 && diff <= 30) {
		return { status: 'upcoming', isReleased: false, isNew: false, daysUntil: diff, badge: `In ${diff}d` };
	}
	if (diff > 30) {
		return { status: 'upcoming', isReleased: false, isNew: false, daysUntil: diff, badge: 'Upcoming' };
	}
	if (diff >= -7) {
		return { status: 'aired', isReleased: true, isNew: true, daysUntil: diff, badge: 'New' };
	}
	return { status: 'aired', isReleased: true, isNew: false, daysUntil: diff, badge: '' };
}
