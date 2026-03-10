export function initSSE(callback) {
    const eventSource = new EventSource('/events');
    eventSource.onmessage = (event) => {
        const data = JSON.parse(event.data);
        callback(data);
    };
    eventSource.onerror = (err) => {
        console.error('SSE Error:', err);
    };
    return eventSource;
}
