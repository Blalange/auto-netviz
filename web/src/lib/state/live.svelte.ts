import { browser } from '$app/environment';
import { invalidateAll } from '$app/navigation';

export interface LiveEventMessage {
	type: string;
	snapshot_id?: number;
	timestamp: string;
	detail?: string;
}

class LiveStreamState {
	connected = $state(false);
	lastEvent = $state<LiveEventMessage | null>(null);
	isPausedForHistory = $state(false);
	private ws: WebSocket | null = null;
	private timer: ReturnType<typeof setTimeout> | null = null;
	private started = false;

	start() {
		if (!browser || this.started) return;
		this.started = true;
		this.connect();
	}

	setHistoricalMode(historical: boolean) {
		this.isPausedForHistory = historical;
	}

	private connect() {
		if (!browser) return;
		const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const wsUrl = `${proto}//${window.location.host}/ws/events`;
		try {
			const ws = new WebSocket(wsUrl);
			this.ws = ws;
			ws.onopen = () => {
				this.connected = true;
			};
			ws.onmessage = (ev) => {
				try {
					const msg = JSON.parse(ev.data) as LiveEventMessage;
					this.lastEvent = msg;
					if (!this.isPausedForHistory) {
						invalidateAll();
					}
				} catch {
					// ignore malformed event
				}
			};
			ws.onclose = () => {
				this.connected = false;
				this.ws = null;
				this.timer = setTimeout(() => this.connect(), 3000);
			};
			ws.onerror = () => {
				ws.close();
			};
		} catch {
			this.connected = false;
			this.timer = setTimeout(() => this.connect(), 5000);
		}
	}

	stop() {
		if (this.timer) clearTimeout(this.timer);
		this.ws?.close();
		this.started = false;
	}
}

export const liveStream = new LiveStreamState();
