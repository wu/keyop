# Messaging Migration Plan

## Overview

This plan outlines the steps to introduce the versioned `Envelope` and `Payload` architecture while maintaining
compatibility with existing producers and consumers.

## Step 1: Core Envelope Types (Phase 1)

Introduce `Envelope` and `Payload` structs in `core/envelope.go`. These will coexist with the current `Message` struct.

## Step 2: Adapters (Phase 1)

Add methods to convert between `Message` and `Envelope`.

- `Envelope.ToMessage()`: Converts `Envelope` (v1) to the legacy `Message` struct for old consumers.
- `NewEnvelopeFromMessage(m Message)`: Wraps a legacy message into a new `v1` envelope.

## Step 3: Messenger Integration (Phase 2 & 3)

Modify `Messenger.Send` and `Messenger.SubscribeExtended`:

- `Send`: Always wrap the outgoing payload in an `Envelope` before serializing to JSON for the `PersistentQueue`.
- `SubscribeExtended`:
    1. Read from the queue (already in `Envelope` format).
    2. Perform retry/dead-letter logic using the `RetryCount` field in the `Envelope`.
    3. Support typed payload unmarshaling using the `UnmarshalPayload` method and the `payload-type` header.
    4. If a legacy consumer is used, unwrap the `Envelope` to a `Message` before calling the handler.

## Step 4: Retry and Dead-Letter Logic (Phase 2)

Implement a configurable retry policy. When a consumer handler fails:

1. Increment the `RetryCount` in the `Envelope`.
2. If `RetryCount > MaxRetryAttempts`, move the message to a special dead-letter channel.
3. If DLQ enqueue fails, the message is NOT acknowledged in the source queue, ensuring no message loss.
4. If not, retry with exponential backoff (1s base, 5m max, with jitter).

## Step 5: Concurrency Audit (Phase 4)

Verify that `Messenger` and `Queue` locks are minimized during I/O.

- Check `Messenger.Send`: `Enqueue` call is outside `m.mutex`.
- Check `PersistentQueue.Enqueue`: Appending to the log file is outside `pq.mu`.
- Check `PersistentQueue.Dequeue` and `Ack`: State updates (small JSON writes) are performed under `pq.mu` to ensure
  reader consistency.
- Ensure `statsMutex` in `Messenger` is only held for numeric updates.

## Safe Defaults

- `MaxRetryAttempts`: 5
- `RetryBackoffBase`: 1 second
- `RetryBackoffMax`: 5 minutes
- `DeadLetterChannel`: `_dlq.<original_channel>`

## Rollout

1. Deploy `core/envelope.go` and its tests.
2. Deploy the `Messenger` refactor. Existing producers/consumers will work because the internal format change (wrapping
   in `Envelope`) is handled automatically.
3. Gradually migrate producers/consumers to use the new typed `Envelope` API (if a new API is introduced).
