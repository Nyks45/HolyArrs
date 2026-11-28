interface AudioGraph {
	context: AudioContext;
	source: MediaElementAudioSourceNode;
	gain: GainNode;
	limiter: DynamicsCompressorNode;
}

const graphs = new WeakMap<HTMLVideoElement, AudioGraph>();

export async function setAudioBoost(video: HTMLVideoElement, percent: number): Promise<void> {
	let graph = graphs.get(video);
	if (!graph && percent <= 100) return;
	if (!graph) {
		const context = new AudioContext();
		try {
			const gain = context.createGain();
			const limiter = context.createDynamicsCompressor();
			limiter.threshold.value = -3;
			limiter.knee.value = 6;
			limiter.ratio.value = 12;
			const source = context.createMediaElementSource(video);
			source.connect(gain).connect(context.destination);
			graph = { context, source, gain, limiter };
			graphs.set(video, graph);
		} catch (error) {
			await context.close();
			throw error;
		}
	}
	const boost = Math.max(100, Math.min(300, percent)) / 100;
	const previous = graph.gain.gain.value;
	if ((previous > 1) !== (boost > 1)) {
		graph.gain.disconnect();
		if (boost > 1) graph.gain.connect(graph.limiter);
		else graph.gain.connect(graph.context.destination);
		graph.limiter.disconnect();
		if (boost > 1) graph.limiter.connect(graph.context.destination);
	}
	graph.gain.gain.value = boost;
	if (graph.context.state === 'suspended') await graph.context.resume();
}

export function releaseAudioBoost(video: HTMLVideoElement): void {
	const graph = graphs.get(video);
	if (!graph) return;
	graph.source.disconnect();
	graphs.delete(video);
	void graph.context.close().catch(() => {});
}
