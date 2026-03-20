package owntracks

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	_ "image/png" // register PNG decoder
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// mapZoom is the OSM zoom level used for map downloads.
// At zoom 15 and latitude ~47.5°, 1 pixel ≈ 3.2 m — a good neighborhood-scale view.
const mapZoom = 15
const mapTileSize = 256
const mapGridHalf = 1 // 1 → 3×3 grid of tiles

var mapHTTPClient = &http.Client{}

// latLonToTile converts geographic coordinates to OSM tile indices at the given zoom.
func latLonToTile(lat, lon float64, zoom int) (int, int) {
	n := math.Pow(2, float64(zoom))
	x := int(math.Floor((lon + 180.0) / 360.0 * n))
	latRad := lat * math.Pi / 180.0
	y := int(math.Floor((1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n))
	return x, y
}

// latLonToPixel returns the pixel coordinates of lat/lon within the stitched image
// whose top-left tile is (originX, originY).
func latLonToPixel(lat, lon float64, zoom, originX, originY int) (int, int) {
	n := math.Pow(2, float64(zoom))
	latRad := lat * math.Pi / 180.0
	txFrac := (lon+180.0)/360.0*n - float64(originX)
	tyFrac := (1.0-math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi)/2.0*n - float64(originY)
	return int(math.Round(txFrac * mapTileSize)), int(math.Round(tyFrac * mapTileSize))
}

var (
	cacheDirOnce sync.Once
	cacheDirPath string
	cacheDirErr  error
)

func mapCacheDir() (string, error) {
	cacheDirOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			cacheDirErr = err
			return
		}
		dir := filepath.Join(home, ".keyop", "maps")
		if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // user-owned cache dir
			cacheDirErr = err
			return
		}
		cacheDirPath = dir
	})
	return cacheDirPath, cacheDirErr
}

// getOrFetchBaseMap returns the raw (no dot) stitched tile PNG, cached by centre-tile.
// Individual tiles are cached via fetchOrCacheTile so they are shared with the history view.
func getOrFetchBaseMap(cx, cy int) ([]byte, error) {
	cacheDir, err := mapCacheDir()
	if err != nil {
		return nil, err
	}
	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("%d_%d_%d.png", mapZoom, cx, cy))
	if data, err := os.ReadFile(cacheFile); err == nil { //nolint:gosec // path is constructed from trusted tile coords
		return data, nil
	}

	grid := 2*mapGridHalf + 1
	composite := image.NewRGBA(image.Rect(0, 0, mapTileSize*grid, mapTileSize*grid))
	for dy := -mapGridHalf; dy <= mapGridHalf; dy++ {
		for dx := -mapGridHalf; dx <= mapGridHalf; dx++ {
			tileData, err := fetchOrCacheTile(mapZoom, cx+dx, cy+dy)
			if err != nil {
				return nil, fmt.Errorf("fetch tile %d/%d/%d: %w", mapZoom, cx+dx, cy+dy, err)
			}
			img, _, err := image.Decode(bytes.NewReader(tileData))
			if err != nil {
				return nil, fmt.Errorf("decode tile %d/%d/%d: %w", mapZoom, cx+dx, cy+dy, err)
			}
			px := (dx + mapGridHalf) * mapTileSize
			py := (dy + mapGridHalf) * mapTileSize
			draw.Draw(composite,
				image.Rect(px, py, px+mapTileSize, py+mapTileSize),
				img, image.Point{}, draw.Src)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, composite); err != nil {
		return nil, err
	}
	data := buf.Bytes()
	_ = os.WriteFile(cacheFile, data, 0o600) //nolint:gosec // path is constructed from trusted tile coords
	return data, nil
}

// drawLocationDot paints a purple dot with a white border at the given pixel.
func drawLocationDot(img *image.RGBA, px, py int) {
	const outerR = 10
	const innerR = 7
	purple := color.RGBA{R: 139, G: 92, B: 246, A: 255}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	bounds := img.Bounds()
	for y := py - outerR; y <= py+outerR; y++ {
		for x := px - outerR; x <= px+outerR; x++ {
			if x < bounds.Min.X || x >= bounds.Max.X || y < bounds.Min.Y || y >= bounds.Max.Y {
				continue
			}
			dx, dy := x-px, y-py
			d2 := dx*dx + dy*dy
			switch {
			case d2 <= innerR*innerR:
				img.SetRGBA(x, y, purple)
			case d2 <= outerR*outerR:
				img.SetRGBA(x, y, white)
			}
		}
	}
}

// getOrFetchMap returns a PNG with the tile base and a purple location dot.
// The base tiles are cached; the dot is drawn fresh each call so it stays accurate
// if the location moves within the same tile grid.
func getOrFetchMap(lat, lon float64) ([]byte, error) {
	cx, cy := latLonToTile(lat, lon, mapZoom)

	base, err := getOrFetchBaseMap(cx, cy)
	if err != nil {
		return nil, err
	}

	// Decode base into an RGBA canvas.
	baseImg, _, err := image.Decode(bytes.NewReader(base))
	if err != nil {
		return nil, fmt.Errorf("decode base map: %w", err)
	}
	rgba := image.NewRGBA(baseImg.Bounds())
	draw.Draw(rgba, rgba.Bounds(), baseImg, image.Point{}, draw.Src)

	// Draw dot at the exact sub-tile pixel.
	originX, originY := cx-mapGridHalf, cy-mapGridHalf
	px, py := latLonToPixel(lat, lon, mapZoom, originX, originY)
	drawLocationDot(rgba, px, py)

	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fetchOSMTile(zoom, x, y int) ([]byte, error) {
	url := fmt.Sprintf("https://tile.openstreetmap.org/%d/%d/%d.png", zoom, x, y)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "keyop/1.0 home-automation (personal use)")
	resp, err := mapHTTPClient.Do(req) //nolint:gosec // URL is a fixed OSM tile endpoint
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tile server returned %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// getMapForCurrentLocation queries the latest GPS fix and returns a
// base64-encoded PNG centred on that location with a purple location dot.
func (svc *Service) getMapForCurrentLocation() (map[string]any, error) {
	if svc.db == nil || *svc.db == nil {
		return map[string]any{"map": ""}, nil
	}
	db := *svc.db

	var lat, lon float64
	row := db.QueryRow(`SELECT lat, lon FROM gps_locations ORDER BY timestamp DESC LIMIT 1`)
	if err := row.Scan(&lat, &lon); err != nil {
		if err == sql.ErrNoRows {
			return map[string]any{"map": ""}, nil
		}
		return nil, fmt.Errorf("gps map: query location: %w", err)
	}

	data, err := getOrFetchMap(lat, lon)
	if err != nil {
		return nil, fmt.Errorf("gps map: build map: %w", err)
	}
	return map[string]any{"map": base64.StdEncoding.EncodeToString(data)}, nil
}

// ── History map ───────────────────────────────────────────────────────────────

type gpsPoint struct{ lat, lon float64 }

// fetchOrCacheTile fetches a single OSM tile, caching it as tile_{z}_{x}_{y}.png.
func fetchOrCacheTile(zoom, x, y int) ([]byte, error) {
	cacheDir, err := mapCacheDir()
	if err != nil {
		return nil, err
	}
	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("tile_%d_%d_%d.png", zoom, x, y))
	if data, err := os.ReadFile(cacheFile); err == nil { //nolint:gosec
		return data, nil
	}
	data, err := fetchOSMTile(zoom, x, y)
	if err != nil {
		return nil, err
	}
	_ = os.WriteFile(cacheFile, data, 0o600) //nolint:gosec
	return data, nil
}

// chooseBestZoom returns the highest zoom level where all points fit in a
// reasonable tile grid (≤6×6 tiles).
func chooseBestZoom(minLat, maxLat, minLon, maxLon float64) int {
	for zoom := 15; zoom >= 9; zoom-- {
		x1, y1 := latLonToTile(maxLat, minLon, zoom)
		x2, y2 := latLonToTile(minLat, maxLon, zoom)
		if x2-x1 < 6 && y2-y1 < 6 {
			return zoom
		}
	}
	return 9
}

// drawLine draws a 1-px Bresenham line on img between (x0,y0) and (x1,y1).
func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	e := dx - dy
	bounds := img.Bounds()
	for {
		if x0 >= bounds.Min.X && x0 < bounds.Max.X && y0 >= bounds.Min.Y && y0 < bounds.Max.Y {
			img.SetRGBA(x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * e
		if e2 > -dy {
			e -= dy
			x0 += sx
		}
		if e2 < dx {
			e += dx
			y0 += sy
		}
	}
}

// drawSmallDot draws a small filled circle of radius r at (px, py).
func drawSmallDot(img *image.RGBA, px, py, r int, c color.RGBA) {
	bounds := img.Bounds()
	for y := py - r; y <= py+r; y++ {
		for x := px - r; x <= px+r; x++ {
			if x < bounds.Min.X || x >= bounds.Max.X || y < bounds.Min.Y || y >= bounds.Max.Y {
				continue
			}
			dx, dy := x-px, y-py
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

// getHistoryMapData builds a composite map PNG showing all recorded GPS points.
func (svc *Service) getHistoryMapData() ([]byte, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, nil
	}
	db := *svc.db

	rows, err := db.Query(`SELECT lat, lon FROM gps_locations ORDER BY timestamp ASC LIMIT 10000`)
	if err != nil {
		return nil, fmt.Errorf("gps history: query points: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var points []gpsPoint
	for rows.Next() {
		var p gpsPoint
		if err := rows.Scan(&p.lat, &p.lon); err == nil {
			points = append(points, p)
		}
	}
	if len(points) == 0 {
		return nil, nil
	}

	// Bounding box
	minLat, maxLat := points[0].lat, points[0].lat
	minLon, maxLon := points[0].lon, points[0].lon
	for _, p := range points[1:] {
		if p.lat < minLat {
			minLat = p.lat
		}
		if p.lat > maxLat {
			maxLat = p.lat
		}
		if p.lon < minLon {
			minLon = p.lon
		}
		if p.lon > maxLon {
			maxLon = p.lon
		}
	}

	zoom := chooseBestZoom(minLat, maxLat, minLon, maxLon)

	// Tile range with 1-tile padding
	x1, y1 := latLonToTile(maxLat, minLon, zoom)
	x2, y2 := latLonToTile(minLat, maxLon, zoom)
	const pad = 1
	x1 -= pad
	y1 -= pad
	x2 += pad
	y2 += pad

	cols := x2 - x1 + 1
	rows2 := y2 - y1 + 1
	composite := image.NewRGBA(image.Rect(0, 0, cols*mapTileSize, rows2*mapTileSize))

	// Fill with tiles
	for ty := y1; ty <= y2; ty++ {
		for tx := x1; tx <= x2; tx++ {
			tileData, err := fetchOrCacheTile(zoom, tx, ty)
			if err != nil {
				continue // leave blank on error
			}
			img, _, err := image.Decode(bytes.NewReader(tileData))
			if err != nil {
				continue
			}
			px := (tx - x1) * mapTileSize
			py := (ty - y1) * mapTileSize
			draw.Draw(composite, image.Rect(px, py, px+mapTileSize, py+mapTileSize), img, image.Point{}, draw.Src)
		}
	}

	// Draw track path
	pathColor := color.RGBA{R: 139, G: 92, B: 246, A: 180}
	dotColor := color.RGBA{R: 139, G: 92, B: 246, A: 255}
	startColor := color.RGBA{R: 34, G: 197, B: 94, A: 255} // green start
	endColor := color.RGBA{R: 239, G: 68, B: 68, A: 255}   // red end

	toPixel := func(p gpsPoint) (int, int) {
		return latLonToPixel(p.lat, p.lon, zoom, x1, y1)
	}

	// Lines between consecutive points
	for i := 1; i < len(points); i++ {
		px0, py0 := toPixel(points[i-1])
		px1, py1 := toPixel(points[i])
		drawLine(composite, px0, py0, px1, py1, pathColor)
		// Draw a second adjacent line for 2px width
		drawLine(composite, px0+1, py0, px1+1, py1, pathColor)
	}

	// Dots at each point (small, 2px radius)
	for _, p := range points {
		px, py := toPixel(p)
		drawSmallDot(composite, px, py, 2, dotColor)
	}

	// Larger start (green) and end (red) markers
	if len(points) > 0 {
		px, py := toPixel(points[0])
		drawSmallDot(composite, px, py, 6, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		drawSmallDot(composite, px, py, 4, startColor)
		px, py = toPixel(points[len(points)-1])
		drawSmallDot(composite, px, py, 6, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		drawSmallDot(composite, px, py, 4, endColor)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, composite); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// getHistoryMap returns a base64-encoded PNG showing all recorded GPS points.
func (svc *Service) getHistoryMap() (map[string]any, error) {
	data, err := svc.getHistoryMapData()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return map[string]any{"map": ""}, nil
	}
	return map[string]any{"map": base64.StdEncoding.EncodeToString(data)}, nil
}
