import { describe, expect, it } from 'vitest';
import {
	checkMeasuredRuntimeMismatch,
	classifyDurationMismatch,
	createVideoWatchdogState,
	durationMismatchTier,
	formatShortDuration,
	nextFreezeRecoveryAction,
	tickVideoWatchdog,
	type VideoWatchdogSample
} from './video-watchdog.ts';

function sample(over: Partial<VideoWatchdogSample> = {}): VideoWatchdogSample {
	return {
		nowMs: 1000,
		visible: true,
		paused: false,
		ended: false,
		seeking: false,
		busy: false,
		readyState: 4,
		playbackRate: 1,
		currentTime: 10,
		videoWidth: 1920,
		videoHeight: 1080,
		totalFrames: 240,
		...over
	};
}

describe('video watchdog freeze detection', () => {
	it('ignores background tab (visible=false) without false freeze triggers', () => {
		const st = createVideoWatchdogState();
		expect(tickVideoWatchdog(st, sample({ nowMs: 1000, currentTime: 10, totalFrames: 240 }))).toBe('ok');
		expect(tickVideoWatchdog(st, sample({ nowMs: 6000, visible: false, currentTime: 15, totalFrames: 240 }))).toBe('ok');
		expect(tickVideoWatchdog(st, sample({ nowMs: 7000, visible: true, currentTime: 16, totalFrames: 240 }))).toBe('ok');
	});

	it('returns ok while decoded frames advance normally', () => {
		const st = createVideoWatchdogState();
		expect(tickVideoWatchdog(st, sample({ nowMs: 1000, currentTime: 10, totalFrames: 240 }))).toBe('ok');
		expect(tickVideoWatchdog(st, sample({ nowMs: 2500, currentTime: 11.5, totalFrames: 276 }))).toBe('ok');
		expect(tickVideoWatchdog(st, sample({ nowMs: 4500, currentTime: 13.5, totalFrames: 324 }))).toBe('ok');
	});

	it('detects audio-only videoWidth=0 when playback advances', () => {
		const st = createVideoWatchdogState();
		expect(tickVideoWatchdog(st, sample({ nowMs: 1000, currentTime: 10, videoWidth: 0, videoHeight: 0, totalFrames: 0 }))).toBe('ok');
		expect(tickVideoWatchdog(st, sample({ nowMs: 2200, currentTime: 11.2, videoWidth: 0, videoHeight: 0, totalFrames: 0 }))).toBe('ok');
		expect(tickVideoWatchdog(st, sample({ nowMs: 3100, currentTime: 12.1, videoWidth: 0, videoHeight: 0, totalFrames: 0 }))).toBe('no_video');
	});

	it('detects frozen video frames while currentTime keeps advancing', () => {
		const st = createVideoWatchdogState();
		expect(tickVideoWatchdog(st, sample({ nowMs: 1000, currentTime: 10, totalFrames: 240 }))).toBe('ok');
		expect(tickVideoWatchdog(st, sample({ nowMs: 3000, currentTime: 12.0, totalFrames: 240 }))).toBe('ok');
		expect(tickVideoWatchdog(st, sample({ nowMs: 4000, currentTime: 13.0, totalFrames: 240 }))).toBe('frozen_frames');
	});
});

describe('freeze recovery escalation ladder', () => {
	it('escalates frozen_frames through kick -> reencode_hls -> fallback_source', () => {
		expect(
			nextFreezeRecoveryAction({
				reason: 'frozen_frames',
				attempts: 0,
				pipelineMode: 'hls',
				usingLocalHls: false,
				usingForceReencode: false,
				ffmpegAvailable: true
			})
		).toBe('kick_hls');
		expect(
			nextFreezeRecoveryAction({
				reason: 'frozen_frames',
				attempts: 1,
				pipelineMode: 'hls',
				usingLocalHls: false,
				usingForceReencode: false,
				ffmpegAvailable: true
			})
		).toBe('reencode_hls');
		expect(
			nextFreezeRecoveryAction({
				reason: 'frozen_frames',
				attempts: 2,
				pipelineMode: 'hls',
				usingLocalHls: true,
				usingForceReencode: true,
				ffmpegAvailable: true
			})
		).toBe('fallback_source');
	});

	it('jumps straight to reencode_hls for no_video and codec_error when ffmpeg is available', () => {
		expect(
			nextFreezeRecoveryAction({
				reason: 'no_video',
				attempts: 0,
				pipelineMode: 'hls',
				usingLocalHls: false,
				usingForceReencode: false,
				ffmpegAvailable: true
			})
		).toBe('reencode_hls');
		expect(
			nextFreezeRecoveryAction({
				reason: 'codec_error',
				attempts: 0,
				pipelineMode: 'hls',
				usingLocalHls: false,
				usingForceReencode: true,
				ffmpegAvailable: true
			})
		).toBe('fallback_source');
	});
});

describe('duration mismatch helpers', () => {
	it('classifies movie, episode, and short fake lengths accurately', () => {
		expect(classifyDurationMismatch('movie', 165 * 60, 166 * 60)).toBe('');
		expect(classifyDurationMismatch('movie', 140 * 60, 166 * 60)).toBe('warn');
		expect(classifyDurationMismatch('movie', 599.7, 166 * 60)).toBe('severe');

		expect(classifyDurationMismatch('show', 42.7 * 60, 43 * 60)).toBe('');
		expect(classifyDurationMismatch('show', 30 * 60, 43 * 60)).toBe('warn');
		expect(classifyDurationMismatch('show', 599.7, 43 * 60)).toBe('severe');
		expect(classifyDurationMismatch('show', 8137, 43 * 60)).toBe('severe');
		expect(classifyDurationMismatch('show', 22.7, 43 * 60)).toBe('severe');
		expect(classifyDurationMismatch('show', 23.5 * 60, 24 * 60)).toBe('');
	});

	it('formats short durations and triggers measured mismatch check only on auto-selected sources', () => {
		expect(formatShortDuration(23)).toBe('23s');
		expect(formatShortDuration(2561.5)).toBe('43m');
		expect(formatShortDuration(8137)).toBe('2h 16m');
		expect(durationMismatchTier({ duration_mismatch: 'severe' })).toBe(2);
		expect(durationMismatchTier({ duration_mismatch: 'warn' })).toBe(1);
		expect(durationMismatchTier({})).toBe(0);

		expect(
			checkMeasuredRuntimeMismatch({
				kind: 'show',
				measuredSec: 600,
				expectedRuntimeMin: 43,
				userSelectedCurrent: false,
				autoFallbackEnabled: true,
				autoFallbacks: 0
			})
		).toContain('Wrong video length');
		expect(
			checkMeasuredRuntimeMismatch({
				kind: 'show',
				measuredSec: 600,
				expectedRuntimeMin: 43,
				userSelectedCurrent: true,
				autoFallbackEnabled: true,
				autoFallbacks: 0
			})
		).toBeNull();
	});
});
