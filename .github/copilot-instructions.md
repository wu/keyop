# Copilot Instructions for keyop

## Build, Test, and Lint Commands

- **Build main binaries (no plugins):**
  ```sh
  make build
  ```
- **Build all default plugins:**
  ```sh
  make plugins
  ```
- **Build specific plugins:**
  ```sh
  make build-plugins PLUGINS="rgbMatrix helloWorldPlugin"
  ```
- **Build main binaries and specific plugins:**
  ```sh
  make build PLUGINS="rgbMatrix helloWorldPlugin"
  ```
- **Build macOS Reminders helper (macOS only):**
  ```sh
  make build-reminders-fetcher
  ```
- **Build macOS notify app bundle (macOS only):**
  ```sh
  make build-notify-sender
  make deploy-notify-sender  # installs to /Applications
  ```
- **Build release artifacts (includes macOS helper):**
  ```sh
  make build-release
  ```
- **Run all tests:**
  ```sh
  make test
  # or
  go test ./...
  ```
- **Run tests for a single package:**
  ```sh
  go test ./x/reminders
  ```
- **Lint (requires golangci-lint):**
  ```sh
  make lint
  ```
- **Auto-fix lint and format:**
  ```sh
  make lint-fix
  ```
- **Format code:**
  ```sh
  make fmt
  ```
- **Run messenger benchmark:**
  ```sh
  make bench MESSAGES=10000
  ```
- **Generate package docs (requires gomarkdoc):**
  ```sh
  make docs
  ```

## High-Level Architecture

- **Core** (`core/`): Implements the event-driven framework, including service lifecycle, message passing, persistent
  queues, payload registry, preprocessing, and dependency injection.
- **Services** (`x/`): All built-in services live under `x/` as regular Go packages. Each service implements the
  `Service` interface (`Check`, `ValidateConfig`, `Initialize`). Examples include monitors (CPU, disk, memory, ping,
  SSL), integrations (weather, tides, Kodi, Slack, GitHub, RSS), macOS-specific services (Reminders, Bluetooth
  battery, idle, notify, txtmsg, speak), and web UI tabs (notes, tasks, flashcards, movies, attachments, journal).
- **Plugins** (`plugins/`): Optional runtime extensions (e.g., Homekit, RGB matrix, helloWorld) built as Go plugins
  (`.so` files) and loaded at runtime. Each has its own `Makefile`.
- **Messenger** (`core/messenger.go`): Routes `core.Message` structs between services via named channels backed by a
  `PersistentQueue`. Supports retry with exponential back-off, a dead-letter queue (DLQ), stats, and a
  `PayloadRegistry` for typed message payloads.
- **Persistent Queue** (`core/queue.go`): Durable, file-backed message queue supporting multiple concurrent readers
  with ack/seek semantics. Backs every channel in the Messenger.
- **Payload Registry** (`core/payload.go`): Thread-safe registry mapping `DataType` strings to typed Go structs.
  Services register their payload types at init time; the Messenger decodes them automatically.
- **Preprocessing** (`core/preprocess.go`, `core/preprocess_messenger.go`): `PreprocessMessenger` wraps
  `MessengerApi` and applies configurable `sub_preprocess` / `pub_preprocess` condition rules — filtering,
  transforming, or re-routing messages without changing service code.
- **Dependencies** (`core/dependencies.go`): Struct-based dependency injection container carrying the logger,
  OS provider, messenger, state store, and context/cancel for each service.
- **State Store** (`core/state.go`): `FileStateStore` persists JSON state per service to `~/.keyop/data`.
- **Terminal UI** (`cmd/tui.go`): Optional TUI for monitoring system state.
- **Web UI** (`x/webui/`): Optional web interface for monitoring and configuration, served from embedded static
  assets. Individual services contribute tabs via the web UI extension points.
- **WebSocket transport** (`x/webSocketClient`, `x/webSocketServer`, `x/webSocketProtocol`): Bridges channels
  across hosts using a shared wire protocol defined in `x/webSocketProtocol`.
- **Self-update** (`cmd/selfupdate.go`): Built-in self-update command for downloading new releases.
- **Docker**: Multi-stage Dockerfiles for building and running minimal images, with support for plugins and web UI
  static assets.

## Key Conventions

- **Plugin Build**: Each plugin has its own `Makefile` with `build` and `clean` targets. Use
  `make build-plugins PLUGINS="plugin1 plugin2"` to build specific plugins.
- **Configuration**: YAML config files, typically under `~/.keyop/conf` or as specified in Docker images.
- **macOS Reminders**: The `x/reminders` package requires a Swift helper binary (`reminders_fetcher`), built with
  `make build-reminders-fetcher` and run only on macOS 14+.
- **macOS Notifications**: The `x/notify` package uses a native app bundle (`keyop-notify.app`). Build with
  `make build-notify-sender` and install with `make deploy-notify-sender` (macOS only).
- **Message Format**: All inter-service/plugin messages use the `core.Message` struct (see `core/messenger.go`).
  Legacy envelope format is handled automatically for backward compatibility.
- **Typed Payloads**: Services register typed payload structs via `PayloadRegistry`. Set `Message.DataType` and
  `Message.Data` to send; the registry decodes on receipt. Register payloads in `payloads.go` at package `init`.
- **Persistent State**: Services/plugins persist state using the `StateStore` interface (see `core/state.go`).
- **Testing**: Integration tests for platform-specific services (e.g., macOS Reminders) require the helper binary
  and are skipped in CI. Run a single package with `go test ./x/reminders`.
- **Logging**: Uses a `core.Logger` interface (backed by `slog`) with color output in console mode, or logs to
  `~/.keyop/logs` by default.
- **Timezone**: Defaults to America/Los_Angeles; falls back to UTC if unavailable.
- **Web UI**: Static assets are embedded in the binary and copied to `/webui-static` in Docker images.

## Service Structure

- Store main service code in "service.go"
- Store sqlite code in "sqlite.go"
- Store web server code in "webui.go"
- Extract reusable code into domain-specific files (e.g., "aurora.go" for aurora-related functionality).
- Store package-level docs in "doc.go" with a package comment.
- Store payload definitions and registration in "payloads.go"
- Store utilities that are used by multiple packages in a file with the same name as the package, e.g. "sun/sun.go"
- Store html and css in package subdirectory 'resources'

## Creating Web UI Services (Tabs)

Services that provide a Web UI tab must implement the WebUI extension interfaces. This guide covers the patterns learned
from implementing the links tab.

### Package Organization

A WebUI service should have this structure:

```
x/yourservice/
├── doc.go                    # Package documentation
├── service.go                # Service lifecycle (Check, ValidateConfig, Initialize)
├── webui.go                  # WebUI integration (interfaces and handlers)
├── yourservice.go            # Main business logic (e.g., sqlite.go, parse.go, etc.)
├── errors.go                 # Custom error types (optional)
└── resources/
    ├── yourservice.js        # ES6 module with export async function init
    ├── yourservice.css       # Styles (use CSS theme variables)
    └── yourservice.html      # HTML template (can be embedded in webui.go)
```

### Service Implementation

**service.go** must implement `core.Service`:

```go
type Service struct {
Deps core.Dependencies
Cfg  core.ServiceConfig
// ... other fields
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) *Service {
return &Service{Deps: deps, Cfg: cfg}
}

func (svc *Service) Check() error { return nil }
func (svc *Service) ValidateConfig() []error { return nil }
func (svc *Service) Initialize() error { return nil }
```

### WebUI Interfaces

**webui.go** must implement these interfaces:

1. **TabProvider** - Define the tab that appears in the UI:
   ```go
   func (svc *Service) WebUITab() webui.TabInfo {
       return webui.TabInfo{
           ID:    "yourservice",
           Title: "🎯",  // Use emoji icon
           Content: `<div id="yourservice-container">...HTML...</div>`,
           JSPath: "/api/assets/yourservice/yourservice.js",
       }
   }
   ```

2. **ActionProvider** - Handle actions from the frontend:
   ```go
   func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
       switch action {
       case "my-action":
           return svc.myAction(params)
       default:
           return nil, fmt.Errorf("unknown action: %s", action)
       }
   }
   ```

3. **AssetProvider** - Serve CSS, JS, and other resources:
   ```go
   //go:embed resources
   var embeddedAssets embed.FS

   func (svc *Service) WebUIAssets() http.FileSystem {
       sub, _ := fs.Sub(embeddedAssets, "resources")
       return http.FS(sub)
   }
   ```

4. **RouteProvider** (optional) - Register custom HTTP routes:
   ```go
   func (svc *Service) RegisterRoutes(mux *http.ServeMux) {
       mux.HandleFunc("GET /api/yourservice/custom", svc.handleCustomRoute)
   }
   ```

### JavaScript Module Pattern

**yourservice.js** must use proper ES6 module syntax and be loaded by the webui/app.js:

```javascript
let container = null;

export async function init(container) {
  // Called when the tab is first loaded
  setupEventListeners();
  await loadData();
}

async function callAction(action, params) {
  const response = await fetch(`/api/tabs/yourservice/action/${action}`, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(params),
  });
  if (!response.ok) throw new Error(response.statusText);
  return await response.json();
}

async function loadData() {
  try {
    const result = await callAction('get-data', {});
    render(result);
  } catch (err) {
    console.error('Error:', err);
  }
}

function setupEventListeners() {
  const btn = document.getElementById('my-button');
  btn?.addEventListener('click', handleClick);
}

async function handleClick() {
  try {
    await callAction('do-something', {});
    await loadData();  // Refresh
  } catch (err) {
    alert('Error: ' + err.message);
  }
}

// Export additional lifecycle functions (optional)
export function focusItems() {
  // Called when user presses ArrowDown on the tab
}

export function canReturnToTabs() {
  // Called when user presses ArrowUp from items
  return true;
}
```

### CSS Styling

**yourservice.css** must scope all styles under the container ID and use CSS theme variables:

```css
#yourservice-container {
  width: 100%;
  height: 100%;
  display: flex;
  overflow: hidden;
}

#yourservice-container .sidebar {
  width: 200px;
  border-right: 1px solid var(--border);
  padding: 12px;
  background-color: var(--bg);
  overflow-y: auto;
}

#yourservice-container .sidebar-item {
  padding: 8px;
  background: var(--item-bg);
  border: 1px solid var(--border);
  color: var(--text);
  cursor: pointer;
  transition: background 0.2s;
}

#yourservice-container .sidebar-item:hover {
  background: var(--hover-bg);
}

#yourservice-container input {
  background: var(--item-bg);
  color: var(--text);
  border: 1px solid var(--border);
}

#yourservice-container input:focus {
  outline: none;
  border-color: var(--accent, #007acc);
  box-shadow: 0 0 0 2px var(--accent-dim, rgba(0, 122, 204, 0.2));
}
```

**Available CSS theme variables:**

- `--bg` - Primary background
- `--item-bg` - Item/card background
- `--border` - Border color
- `--text` - Primary text color
- `--text-muted` - Muted/secondary text
- `--hover-bg` - Hover state background
- `--accent` - Accent color (blue)
- `--accent-dim` - Accent with opacity
- `--shadow` - Shadow color

### API Endpoint Pattern

All frontend-to-backend communication uses this endpoint pattern:

```
POST /api/tabs/{tabId}/action/{action}

Headers:
  Content-Type: application/json

Request Body:
  JSON object with action parameters

Response:
  JSON object with results
```

Example:

```javascript
// Frontend
const result = await fetch('/api/tabs/links/action/add-link', {
  method: 'POST',
  headers: {'Content-Type': 'application/json'},
  body: JSON.stringify({url: 'https://example.com', name: 'Example'})
});

// Backend (service.go)
func(svc * Service)
addLinkAction(params
map[string]
any
)
(any, error)
{
  url, _
:
  = params["url"].(string)
  name, _
:
  = params["name"].(string)
  // ... do work ...
  return map[string]
  any
  {
    "id"
  :
    id
  }
,
  nil
}
```

### Tab Ordering

New tabs are positioned by their order in the `tabOrder` map in `x/webui/service.go`:

```go
tabOrder := map[string]int{
"dashboard": 0,
"alerts":    1,
// ... other tabs ...
"yourservice": 10, // Position in the tab bar
}
```

Update the same order in `x/webui/resources/js/app.js`:

```javascript
const tabOrder = {
  'dashboard': 0,
  'alerts': 1,
  // ... other tabs ...
  'yourservice': 10,
};
```

### Service Registration

Register the service in `x/run/registry.go`:

```go
import "keyop/x/yourservice"

var serviceRegistry = map[string]ServiceFactory{
// ... other services ...
"yourservice": func (deps core.Dependencies, cfg core.ServiceConfig) core.Service {
return yourservice.NewService(deps, cfg)
},
}
```

### Database Layer

If using SQLite (recommended pattern):

1. Create `yourservice.go` or `sqlite.go` with database functions
2. Use modernc.org/sqlite (pure Go, no C dependencies)
3. Follow the pattern:
  - `openYourserviceDB()` - Open and initialize DB
  - `initYourserviceDB()` - Create schema
  - `addItem()`, `listItems()`, `deleteItem()` - CRUD operations
4. Handle errors: Always close DB connections with `defer func() { _ = db.Close() }()`

Example:

```go
func openYourserviceDB(dbPath string) (*sql.DB, error) {
db, err := sql.Open("sqlite", dbPath)
if err != nil {
return nil, err
}
if err := initYourserviceDB(db); err != nil {
_ = db.Close()
return nil, err
}
return db, nil
}

func initYourserviceDB(db *sql.DB) error {
schema := `
    CREATE TABLE IF NOT EXISTS items (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        created_at DATETIME NOT NULL
    );
    `
_, err := db.Exec(schema)
return err
}

func addItem(dbPath, name string) error {
db, err := openYourserviceDB(dbPath)
if err != nil {
return err
}
defer func () { _ = db.Close() }()

_, err = db.Exec(
`INSERT INTO items (name, created_at) VALUES (?,?)`,
name, time.Now().UTC().Format(time.RFC3339),
)
return err
}
```

### Common Patterns

**Split Layout** (sidebar + content):

```javascript
// In CSS
#container
{
  display: flex;
  height: 100 %;
}
#sidebar
{
  width: 200
  px;
  border - right
:
  1
  px
  solid
  var (
  --border
)
  ;
}
#content
{
  flex: 1;
  overflow - y
:
  auto;
}

// In HTML
<div id="container">
  <div id="sidebar">Filters...</div>
  <div id="content">Items...</div>
</div>
```

**Search + Sort**:

```javascript
<input id="search" placeholder="Search...">
  <select id="sort">
    <option value="date-desc">Newest</option>
    <option value="date-asc">Oldest</option>
  </select>

  // JavaScript
  const search = document.getElementById('search').value;
  const sort = document.getElementById('sort').value;
  const result = await callAction('list-items', {search, sort});
```

**Tag Filtering**:

```javascript
// Backend: Return tag counts
async function getTagCounts(params) {
  // ... query DB ...
  return map[string]
  int
  {
    "tag1"
  :
    5, "tag2"
  :
    3
  }
}

// Frontend: Render clickable tags
<div class="tag" onclick="filterByTag('tag1');">tag1 (5)</div>
```

**Error Handling**:

```javascript
try {
  const result = await callAction('action', {});
} catch (err) {
  console.error('Action failed:', err);
  alert('Error: ' + err.message);
}
```

**Bulk Operations with Feedback**:
When implementing bulk import/add operations, return count information and optionally failed items:

```go
// Backend
return map[string]any{
"imported": 10,
"updated": 5,
"failed": 2,
"failed_urls": []string{"url1", "url2"}, // Optional detail
}, nil

// Frontend
const result = await callAction('bulk-import', { text: input });
let msg = `Imported ${result.imported}, updated ${result.updated}`;
if (result.failed > 0) {
msg += `\n\nFailed: ${result.failed}`;
if (result.failed_urls?.length) {
msg += '\n' + result.failed_urls.join('\n');
}
}
alert(msg)
```

**Favicon/Icon Fetching**:
For services that fetch external resources (favicons, images), do it asynchronously after initial save:

```go
// Background goroutine
go func (id, url string) {
if icon, err := FetchIcon(url); err == nil {
_ = UpdateIcon(dbPath, id, icon)  // Ignore errors in goroutine
}
}(id, url)
```

**Date/Time Handling**:
JavaScript datetime-local inputs expect local time, not UTC. Convert properly:

```javascript
// ISO → local time for display
function dateToLocalInput(isoString) {
  if (!isoString) return '';
  const date = new Date(isoString);
  return date.toISOString().slice(0, 16);  // YYYY-MM-DDTHH:mm
}

// Local time → ISO for storage
function localInputToISO(localInput) {
  if (!localInput) return '';
  const parts = localInput.split('T');
  const date = new Date(parts[0] + 'T' + parts[1] + ':00Z');
  return date.toISOString();
}
```

**Database Path and Directory Management**:
Always expand `~` in paths and ensure directories exist:

```go
func (svc *Service) getDataDir() string {
    homeDir, _ := os.UserHomeDir()
    dataDir := filepath.Join(homeDir, ".keyop", "data", "yourservice")
    os.MkdirAll(dataDir, 0700)
    return dataDir
}

### Testing Checklist

Before deploying a WebUI service:

- [ ] Service implements all required interfaces (Service, TabProvider, ActionProvider, etc.)
- [ ] JavaScript uses proper ES6 module pattern with `export async function init`
- [ ] CSS uses theme variables, not hardcoded colors
- [ ] CSS scopes all rules under container ID
- [ ] API actions use correct endpoint pattern: `/api/tabs/{tabId}/action/{action}`
- [ ] JavaScript calls `callAction()` with proper error handling
- [ ] Tab ID added to `tabOrder` map in both Go and JavaScript
- [ ] Service registered in `x/run/registry.go`
- [ ] Binary builds without errors
- [ ] All existing tests still pass
- [ ] CSS looks good in dark theme
- [ ] Actions load data and handle user input
- [ ] Forms validate inputs before submitting

## Integrating Services with Full-Text Search

Services can be integrated with the search tab to make their data searchable.This allows users to search across notes, links, and other services.

### Overview

The search system uses Bleve for full-text indexing and provides:
- Cross-service search with type filtering
- Configurable field storage and searching
- Automatic indexing of service data
- Real-time updates when data changes

### Step 1: Implement IndexProvider in Your Service

Add an `IndexProvider` interface implementation to your service's `webui.go`:

```go
// IndexProvider implementation
func (svc *Service) BulkIndex() ([]search.SearchableDocument, error) {
    // Fetch all items from your service
    // Return array of SearchableDocument with all fields populated
    docs := make([]search.SearchableDocument, 0)
    
    // Example: for each item in your service
    docs = append(docs, search.SearchableDocument{
        ID:         fmt.Sprintf("yourservice:%s", item.ID),
        SourceType: "yourservice",
        SourceID:   item.ID,
        Title:      item.Title,
        Body:       item.Body,
        Tags:       item.Tags,
        UpdatedAt:  item.UpdatedAt,
    })
    
    return docs, nil
}
```

**Key points:**

- ID must be formatted as `{serviceType}:{itemID}`
- SourceType should match your service name (e.g., "notes", "links")
- SourceID should be the unique ID of the item
- Title is the primary searchable field
- Body/content is searchable and displayed in snippets
- Tags should be an array of tag strings
- UpdatedAt should be an ISO 8601 timestamp
- All string IDs from the service should be preserved as-is

### Step 2: Emit Search Events on Changes

In your service's action handlers, emit `SearchIndexEvent` when data changes:

```go
// After creating/updating an item
svc.Deps.Messenger.Send(core.Message{
To:       "search",
From:     "yourservice",
Subject:  "index-events",
DataType: "search:SearchIndexEvent",
Data: search.SearchIndexEvent{
Type: search.EventTypeUpsert,
Doc: search.SearchableDocument{
ID:         fmt.Sprintf("yourservice:%s", item.ID),
SourceType: "yourservice",
SourceID:   item.ID,
Title:      item.Title,
Body:       item.Body,
Tags:       item.Tags,
UpdatedAt:  item.UpdatedAt,
},
},
})

// After deleting an item
svc.Deps.Messenger.Send(core.Message{
To:       "search",
From:     "yourservice",
Subject:  "index-events",
DataType: "search:SearchIndexEvent",
Data: search.SearchIndexEvent{
Type: search.EventTypeDelete,
ID:   fmt.Sprintf("yourservice:%s", item.ID),
},
})
```

### Step 3: Wire the IndexProvider in x/run/run.go

The search service automatically discovers and registers IndexProviders:

```go
// In x/run/run.go, add your service to the provider registration
if indexProvider, ok := svc.(search.IndexProvider); ok {
searchSvc.RegisterIndexProvider(indexProvider)
}
```

This is handled automatically - just ensure your service implements `search.IndexProvider`.

### Step 4: Handle Navigation from Search Results

When users click on a search result, a `navigate-to-item` event is dispatched to the target tab. Your service's
JavaScript module should listen for this:

```javascript
// In your service's init function (export async function init(container))
container.addEventListener('navigate-to-item', (e) => {
  const {itemId, sourceType} = e.detail;
  if (itemId && sourceType === 'yourservice') {
    // Navigate to the item in your service
    selectItem(itemId);
    // Show the item view (not just the list)
  }
});
```

**Important:** Your service's JavaScript must be an ES6 module with `export async function init(container)`, not an
IIFE. The container parameter is the tab-content wrapper element.

### Step 5: Update Navigation in Other Services

If another service needs to navigate to items, you can send navigation events:

```javascript
// Navigate to a note from links or other services
container.dispatchEvent(new CustomEvent('navigate-to-item', {
  detail: {itemId: '3627', sourceType: 'notes'}
}));
```

Then click the tab to switch to it.

### Search Field Storage

**Critical:** For fields to appear in search results, they must have `Store: true` in the Bleve field mapping:

```go
// In search/index.go buildIndexMapping()
docMapping.AddFieldMappingsAt("title", titleFieldMapping)
```

Without `Store: true`, fields are indexed but not stored, so they won't appear in search result snippets or metadata.
The search service automatically handles this for all standard fields (title, body, tags, sourceType, etc.).

### Type Coercion for IDs

**Important:** When receiving IDs from search results in JavaScript, convert string IDs to the correct type for your
backend API:

```javascript
// Search returns string IDs, but your API might expect numbers
const itemId = parseInt(itemId, 10) || itemId; // For numeric IDs
selectItem(itemId); // Pass the converted ID
```

Check your service's action handlers to see what type they expect for IDs.

### Common Issues

1. **Note not appearing in list after selection:** Clear filters and reload the full list (without search/tag filters)
   so the note appears in default sort order. Use the pinned indicator if the note is on a different page.

2. **Fields not appearing in search results:** Ensure all fields have `Store: true` in the Bleve mapping and that
   `Dynamic: false` is set (otherwise dynamic fields won't be stored).

3. **Tags display with wrong colors:** Use hardcoded hex colors in CSS instead of CSS variables that might not be
   available. Example: use `#8a52da` instead of `var(--accent-blue)` for guaranteed consistency.

4. **Navigation event not received:** Ensure your service exports an ESM init function that receives the container. Old
   IIFE patterns won't work - convert to `export async function init(container)`.

## Policy

Policy: Never run git commands (add/commit/push/branch/tag) or modify the repository without explicit, written approval
from the repo owner. If a task requires git, ask first and wait for a clear yes.

Policy: Never modify the configuration without explicit, written approval from the repo owner.

Policy: Do not look for server process ids or attempt to kill processes unless you are explicitly asked to do so by the
repo owner as part of a specific task. You can make requests to the server process. Ask me and I will provide the
local port.

Policy: Always discuss with me before adding backward compatibility or implementing fallback mechanisms.


---

This file summarizes build/test/lint commands, architecture, and key conventions for Copilot and other AI tools. Would
you like to adjust anything or add coverage for additional areas?
