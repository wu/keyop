# ADR: Messaging Contract and Delivery Semantics

## Status

Proposed

## Context

The project currently uses a `Messenger` with a file-based `PersistentQueue` (`core/queue.go`). Messages are retried
indefinitely with exponential backoff on failure in `SubscribeExtended`. The `Message` struct mixes domain-level
fields (e.g., `Text`, `Metric`, `State`) with transport-related fields (e.g., `Uuid`, `Correlation`, `Timestamp`,
`Route`).

## Delivery Guarantees

- **Guarantee**: At-least-once delivery.
- **Scope**: Messages are persisted to disk upon `Send` and acknowledged by consumers only after successful processing.
- **Ordering**: Strict per-channel ordering is maintained by the `PersistentQueue`. However, retries of a failed message
  block subsequent messages in the same channel for that specific consumer.

## Delivery Semantics

1. **Explicit Acknowledgment**: Consumers must process a message successfully before it's acknowledged in the queue
   state.
2. **Retry Policy**:
    - **MaxRetryAttempts**: Defines the number of retries *after* the initial attempt.
    - Default: 5 retries (6 total attempts).
    - Backoff: Exponential backoff with jitter (1s base, 5m max).
3. **Dead-Letter Handling**:
    - Messages exceeding the retry limit are moved to a Dead-Letter Queue (DLQ).
    - DLQ is implemented as a separate channel (e.g., `_dlq.<original_channel>`).
    - Original message metadata (reason for failure, attempt count) is preserved in the DLQ envelope.
    - **DLQ Safety Policy**: If moving a message to the DLQ fails (e.g., disk full), the original message is **NOT
      acknowledged**. It remains in the source queue and will be re-delivered upon subscriber restart or timeout,
      ensuring no message loss.

## Versioned Envelope and Typed Payload

A new `Envelope` struct separates transport-level metadata from the `Payload`.

### Envelope Structure (Version 1)

- `Version`: Envelope schema version.
- `ID`: Unique message identifier (UUID).
- `CorrelationID`: ID for request-response or causal chains.
- `Timestamp`: When the message was produced.
- `Source`: Originating service/hostname.
- `Topic` (formerly `ChannelName`): Destination channel.
- `RetryCount`: Number of times this message has been retried.
- `Trace`: Path taken by the message (loop detection).
- `Headers`: Arbitrary key-value metadata.
- `Payload`: The domain data.

### Payload Model

- The domain payload is an `any` type, allowing for typed support.
- **Typed Payload Registry**: Common event types are managed via a `PayloadRegistry` interface. This allows for runtime
  registration of new types, including those defined by external plugins.
- **Duplicate Registration Policy**:
    - The registry is idempotent for identical registrations.
    - If a different factory is registered for an existing type name, the registry returns
      `ErrPayloadTypeAlreadyRegistered`.
    - Services and plugins should use `core.IsDuplicatePayloadRegistration(err)` to handle these cases gracefully.
- **Payload Type Header**: The `payload-type` header in the envelope is used to identify the specific type for
  unmarshaling.
- **Naming Convention**: Payload types should follow the `<namespace>.<pluginOrDomain>.<event>.vN` convention.
- **Supported Types**:
    - `service.heartbeat.v1` (Canonical): `HeartbeatEvent` (now, uptime, uptimeSeconds) in `x/heartbeat` package.
    - `core.device.status.v1`: `DeviceStatusEvent` (deviceId, status, battery)
    - `core.metric.v1`: `MetricEvent` (name, value, unit)
    - `plugin.helloWorld.greeting.v1`: `GreetingPayload` (message, from) in `helloWorldPlugin`.
- **Legacy Compatibility Aliases**:
    - `heartbeat` -> `service.heartbeat.v1` (maintained for backward compatibility with legacy producers/queues).
    - `device.status` -> `core.device.status.v1`
    - `metric` -> `core.metric.v1`
- **Fallback Behavior**: If a `payload-type` is unknown or its plugin is not loaded, the system gracefully falls back to
  a raw `map[string]any` (or the original payload) without failing message processing. A warning is logged once per
  unknown type.
- **Plugin and Built-in Service Integration**: Plugins and built-in services can implement the `RuntimePlugin` interface
  to register their own payload types during host initialization. This ensures that custom events are recognized by the
  system before any message subscribers start processing.
- Compatibility with the old `Message` struct is maintained via adapters and type aliases in the registry.

## Concurrency and State Safety

- **Lock Minimization**: The system aims to minimize lock hold time. Large I/O operations (such as appending to the
  persistent queue) are performed outside of global locks.
- **Queue Operations**: `PersistentQueue` uses a per-queue mutex to protect reader state and coordinate between
  producers and consumers. While most I/O is performed with minimal lock contention, some state management operations (
  like updating reader offsets) may involve short file I/O while holding the queue lock to ensure consistency.
- **State Mutations**: Global state mutations (e.g., stats updates) use fine-grained locking (`statsMutex`) or atomic
  operations.
- **I/O Boundaries**: Long-running network or disk I/O should be avoided while holding broad mutexes.
- `sync.RWMutex` is preferred for read-heavy state.

## Startup Sequence and Initialization Order

To ensure all message types are recognized before processing starts, the application follows a strict initialization
order:

1. **Registry Creation**: The authoritative `PayloadRegistry` is created (automatically via `core.GetPayloadRegistry()`
   in `core/envelope.go`).
2. **Built-in Registration**: Core payload types (e.g., `core.device.status.v1`, `core.metric.v1`) are registered during
   package initialization.
3. **Dependency Initialization**: Global dependencies (logger, OS provider, messenger) are initialized in
   `util.InitializeDependencies`.
4. **Plugin Loading**: Plugins are discovered and loaded. Each plugin implementing `core.RuntimePlugin` has its
   `RegisterPayloads(registry)` method called to register custom payload types. This is orchestrated in
   `x/run/plugins.go`.
5. **Service Configuration**: Service configurations are loaded, and services are instantiated.
6. **Subscribers Start**: Message subscribers and service loops are started.

### Plugin Registration Failures

If a plugin fails to register its payload types (e.g., due to a naming conflict), a clear error is logged with the
plugin name and the reason. The host continues to load other plugins and services to ensure partial availability,
following the existing "graceful degradation" policy. Unknown payload types from the failed plugin will be handled via
the standard fallback mechanism (raw `map[string]any`).

## Backward Compatibility

- Producers using the old `Message` struct will have their messages automatically wrapped into a `v1` `Envelope`.
- Consumers using old interfaces will receive the `Payload` converted back to a `Message` struct if possible.
- The `PersistentQueue` format remains compatible (line-delimited JSON).
