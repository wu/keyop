# keyop-messenger Migration Plan

This document describes the phased strategy for migrating keyop from its internal
`core.Messenger` / `core.PersistentQueue` implementation to the standalone
`github.com/keyop/keyop-messenger` library.

---

## Background and Motivation

The original messenger (in `core/messenger.go` and `core/queue.go`) is a single-instance,
in-process pub-sub with file-backed queues. The new library adds:

- Clean module boundary with a versioned public API
- Automatic segment compaction
- At-least-once delivery with configurable retry and dead-letter routing
- mTLS WebSocket federation for multi-host deployments
- Audit log for all cross-instance forwarding
- Proper typed payloads (separate from the wide `core.Message` struct)

---

## API Differences

| Concern | Old (`core.MessengerApi`) | New (`*messenger.Messenger`) |
|---|---|---|
| Publish | `Send(core.Message)` — 21-field struct, channel embedded | `Publish(ctx, channel, payloadType, payload)` |
| Subscribe | `Subscribe(ctx, source, channel, svcType, svcName, maxAge, func(Message) error)` | `Subscribe(ctx, channel, subscriberID, func(ctx, Message) error)` |
| Payload registry | `PayloadRegistry` interface with factory functions | `RegisterPayloadType(typeStr, prototype)` |
| Reader state | Exposed via `SetReaderState` / `SeekToEnd` | Automatic per `subscriberID` |
| Preprocessing | Built-in `PreprocessMessenger` wrapper | None — implemented in handler or publish helper |
| Stats | `GetStats() MessengerStats` | Disk-based audit log |
| Lifecycle | Implicit (lazy queue init, no Close) | Explicit `New()` + `Close()` |
| Federation | Not supported | mTLS WebSocket hub-and-spoke |
| Channel names | Any string | `[a-zA-Z0-9._-]+`, max 255 bytes |
| Data dir | `~/.keyop/data` | Configured — this migration uses `~/.keyop/msgs` |

---

## Migration Architecture

### Dual-Messenger During Transition

Both messengers run concurrently during Phases 1–3. Services opt in to the new API
one at a time. `core.Dependencies` carries both:

```
deps.MustGetMessenger()    → *core.Messenger     (old, all services initially)
deps.GetNewMessenger()     → *messenger.Messenger (new, nil until messenger.yaml present)
```

### MessengerBridge (NEW → OLD)

A `core.MessengerBridge` subscribes to configured channels on the new messenger and
republishes on the old messenger. This allows old-messenger subscribers (e.g. graphite)
to receive messages published by already-migrated services.

```
cpuMonitor  ──Publish──▶  new messenger  ──Bridge──▶  old messenger  ──Subscribe──▶  graphite
            (migrated)    ~/.keyop/msgs              ~/.keyop/data               (not yet migrated)
```

The bridge only needs to run when at least one publisher is on the new messenger and at
least one subscriber is still on the old messenger for the same channel. Once all services
for a channel are migrated, remove that channel from the bridge config.

### Payload Design

Services use **properly typed payload structs** — never `core.Message` as a payload.

- For services whose data fits a canonical core type (`core.MetricEvent`,
  `core.AlertEvent`, `core.StatusEvent`, etc.), use those directly.
- For services with unique, rich data models, define a service-specific struct in
  `x/<service>/payloads.go` and register it with `RegisterPayloadType`.

The bridge includes built-in mappers for all canonical core payload types so that
old-messenger subscribers receive correctly-populated `core.Message` fields.

### Per-Host Configuration

Each host that opts in to the new messenger needs a `messenger.yaml` in its
`keyop-config/<host>/` directory. The file is read from `KEYOP_CONF_DIR` (or
`~/.keyop/conf`) at startup, alongside service configs. The `bridge.channels` list
controls which channels are mirrored to the old messenger.

---

## Phase 1 — Foundation (current)

**Goal:** Wire the new messenger and bridge into keyop without changing any service.

**Deliverables:**

1. `go.mod` — `require github.com/keyop/keyop-messenger` with `replace ../keyop-messenger`
2. `core/dependencies.go` — `GetNewMessenger()` / `SetNewMessenger()` accessors
3. `core/messenger_bridge.go` — `MessengerBridge` with built-in core type mappers
4. `x/run/messenger_init.go` — load `messenger.yaml`, create `*messenger.Messenger`,
   register core payload types, create bridge
5. `x/run/cmd.go` — call `initNewMessenger()` before `run()`; close on context done
6. `x/run/config.go` — skip `messenger.yaml` when loading service configs
7. `keyop-config/<host>/messenger.yaml` — per-host opt-in config

**Acceptance criteria:**
- `go build ./...` succeeds
- All existing tests pass
- keyop starts normally on a host with no `messenger.yaml` (new messenger is skipped)
- keyop starts on a host with `messenger.yaml`, new messenger initialises, bridge starts

---

## Phase 2 — Pilot: cpuMonitor

**Goal:** Validate the new API end-to-end with one service.

**Service:** `x/cpuMonitor`
**Payload type:** `core.MetricEvent` (canonical type; `Name` = metric path, `Value` = usage %)
**Target channel:** `metrics`

**Changes:**

- `x/cpuMonitor/service.go` — In `Check()`: if new messenger is available, call
  `newMsgr.Publish(ctx, "metrics", "core.metric.v1", &core.MetricEvent{...})`.
  Otherwise fall back to old `messenger.Send(core.Message{...})`.
- `keyop-config/<pilot-host>/cpu-monitor.yaml` — no config change needed; bridge
  carries messages to graphite on the old messenger.

**Bridge mapper for `core.metric.v1` → `core.Message`:**
```
MetricName = event.Name
Metric     = event.Value
Event      = "metric"
```

**Acceptance criteria:**
- cpuMonitor publishes via new messenger on a configured host
- Graphite receives the metric (via bridge) and forwards to graphite server
- No increase in DLQ entries or dropped messages

---

## Phase 3 — Incremental Migration

**Goal:** Move all remaining services to the new API in dependency order.

**Migration order:**

1. Pure metric publishers: `memoryMonitor`, `pingMonitor`, `diskspace`, `networkio`,
   `battery`, `heartbeat`
2. Integration services (external data): `rss`, `tides`, `weather`, `owntracks`,
   `github`
3. Alert pipeline (migrate together): `statusmon` → `alerts` → `notify` / `slack` /
   `speak`
4. macOS-specific: `reminders`, `bluetooth`, `txtmsg`
5. Web UI services: `notes`, `tasks`, `links`, `movies`, `flashcards`, `journal`
6. WebSocket bridge services: `webSocketClient`, `webSocketServer`

**Per-service checklist:**
- [ ] Define typed payload struct(s) in `x/<service>/payloads.go` (if not a core type)
- [ ] Register payload type(s) with new messenger in `Initialize()`
- [ ] Replace `messenger.Send(core.Message{...})` with `newMsgr.Publish(...)`
- [ ] Replace `messenger.Subscribe(...)` with `newMsgr.Subscribe(...)` — update handler
      signature to `func(ctx context.Context, msg messenger.Message) error`
- [ ] Remove `SubscribeExtended` / `SetReaderState` / `SeekToEnd` calls (automatic now)
- [ ] Replace `PreprocessMessenger` logic with handler middleware or publish helpers
- [ ] When all services on a channel are migrated, remove the channel from
      `bridge.channels` in that host's `messenger.yaml`

---

## Phase 4 — Cleanup

**Goal:** Remove all old messenger code.

**Deletions:**
- `core/messenger.go` (Messenger, FakeMessenger, MessengerApi, MessengerStats,
  legacyEnvelope, UnmarshalMessage)
- `core/queue.go` (PersistentQueue)
- `core/messenger_bridge.go` (bridge no longer needed)
- `core/preprocess_messenger.go` (replaced by handler middleware)
- `core/preprocess.go` — keep condition logic only if still used elsewhere

**Updates:**
- `core/dependencies.go` — remove old `messenger` field; rename `newMessenger` to
  `messenger`; update `MessengerApi` interface (or remove and use `*messenger.Messenger`
  directly)
- `core/payload.go` — factory-based `PayloadRegistry` can be retired; keep shared
  payload structs (`MetricEvent`, `AlertEvent`, etc.)
- `util/dependencies.go` — remove `core.NewMessenger(...)` call
- `x/run/run.go` — remove old payload registration loop that calls `GetPayloadRegistry()`
- `x/run/messenger_init.go` — remove bridge initialisation
- All service `payloads.go` files — remove `RegisterPayloads(reg PayloadRegistry)`
  implementations; keep `RegisterPayloadType(newMsgr)` calls in `Initialize()`

**Acceptance criteria:**
- No references to `core.Messenger`, `PersistentQueue`, `MessengerApi`, or
  `FakeMessenger` remain in production code
- `go build ./...` and `go test ./...` pass clean
- `keyop-config/<host>/messenger.yaml` files have empty `bridge.channels`

---

## Data Directories

| Messenger | Directory | Notes |
|---|---|---|
| Old | `~/.keyop/data/channels/<channel>/` | Queue `.log` files + reader state JSON |
| New | `~/.keyop/msgs/channels/<channel>/` | Segment `.jsonl` files + offset tracking |

The directories are fully separate; both can coexist safely.

---

## Channel Name Audit

Before a service is migrated, verify its channel names pass `[a-zA-Z0-9._-]+`.
Names with underscores (`_`) are valid. Channel names must not contain spaces or
special characters such as `/`, `@`, `#`.

Known channel names in use (audit before Phase 3):
`metrics`, `heartbeat`, `alerts`, `errors`, `status`, `status-ping`, `movie`,
`messenger`, `graphite`, service instance names (e.g. `cpu-monitor`).
