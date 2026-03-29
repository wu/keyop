// Package links provides a service for managing saved links with tags, search, and favicon caching.
//
// The links service stores URLs (with optional name and notes), organizes them with tags,
// and fetches/caches favicons locally. It provides a Web UI tab with search, filtering by tag,
// and sorting capabilities. Users can add links individually or bulk-import from OneTab-style
// clipboard format (newline-separated "url | name | notes" lines).
package links
