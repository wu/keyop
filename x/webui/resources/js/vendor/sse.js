export function initSSE(onMessage, onStatus) {
    const eventSource = new EventSource('/events');
    const notifyStatus = (state) => { if (onStatus) onStatus(state); };

    eventSource.onopen = () => {
        notifyStatus('open');
    };

    eventSource.onmessage = (event) => {
        const data = JSON.parse(event.data);
        if (onMessage) onMessage(data);
    };

    eventSource.onerror = (err) => {
        console.error('SSE Error:', err);
        // readyState: 0 CONNECTING, 1 OPEN, 2 CLOSED
        const state = eventSource.readyState === 0 ? 'reconnecting' : (eventSource.readyState === 1 ? 'open' : 'closed');
        notifyStatus(state);
    };

    // initial status
    const initialState = eventSource.readyState === 0 ? 'reconnecting' : (eventSource.readyState === 1 ? 'open' : 'closed');
    notifyStatus(initialState);

    return eventSource;
}
