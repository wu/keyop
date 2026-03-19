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

func mapCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".keyop", "maps")
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // user-owned cache dir
		return "", err
	}
	return dir, nil
}

// getOrFetchBaseMap returns the raw (no dot) stitched tile PNG, cached by centre-tile.
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
			tileData, err := fetchOSMTile(mapZoom, cx+dx, cy+dy)
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
