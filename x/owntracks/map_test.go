package owntracks

import (
	"image"
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// San Francisco at zoom 15: expected tile roughly (5242, 12663)
func TestLatLonToTile_SanFrancisco(t *testing.T) {
	x, y := latLonToTile(37.7749, -122.4194, 15)
	assert.InDelta(t, 5242, x, 2)
	assert.InDelta(t, 12663, y, 2)
}

func TestLatLonToTile_MultipleZooms(t *testing.T) {
	lat, lon := 37.7749, -122.4194

	x15, y15 := latLonToTile(lat, lon, 15)
	x12, y12 := latLonToTile(lat, lon, 12)
	x9, y9 := latLonToTile(lat, lon, 9)

	// Each zoom level halves the tile count → dividing by 8 should approximate lower zoom
	assert.InDelta(t, x15/8, x12, 2)
	assert.InDelta(t, y15/8, y12, 2)
	assert.InDelta(t, x15/64, x9, 2)
	assert.InDelta(t, y15/64, y9, 2)
}

func TestLatLonToPixel_CenterTile(t *testing.T) {
	lat, lon := 37.7749, -122.4194
	zoom := 15
	cx, cy := latLonToTile(lat, lon, zoom)
	// origin is the top-left tile of the 3×3 grid
	originX := cx - mapGridHalf
	originY := cy - mapGridHalf

	px, py := latLonToPixel(lat, lon, zoom, originX, originY)
	// The point should land in the centre tile (second tile), so pixel coords
	// should be in the range [mapTileSize, 2*mapTileSize].
	assert.GreaterOrEqual(t, px, mapTileSize-5)
	assert.Less(t, px, 2*mapTileSize+5)
	assert.GreaterOrEqual(t, py, mapTileSize-5)
	assert.Less(t, py, 2*mapTileSize+5)
}

func TestDrawLocationDot_CenterIsPurple(t *testing.T) {
	size := 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cx, cy := size/2, size/2
	drawLocationDot(img, cx, cy)

	// The exact centre pixel must be purple (innerR = 7, so (0,0) is well inside)
	purple := color.RGBA{R: 139, G: 92, B: 246, A: 255}
	got := img.RGBAAt(cx, cy)
	assert.Equal(t, purple, got)
}

func TestDrawLocationDot_BorderIsWhite(t *testing.T) {
	size := 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cx, cy := size/2, size/2
	drawLocationDot(img, cx, cy)

	// A pixel at distance 9 from the centre (innerR=7, outerR=10) should be white
	// (9² = 81 > 49 = 7², 81 < 100 = 10²)
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	got := img.RGBAAt(cx+9, cy)
	assert.Equal(t, white, got)
}

func TestDrawLine_Horizontal(t *testing.T) {
	size := 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	lineColor := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	drawLine(img, 10, 32, 50, 32, lineColor)

	for x := 10; x <= 50; x++ {
		got := img.RGBAAt(x, 32)
		assert.Equal(t, lineColor, got, "pixel (%d, 32) should be red", x)
	}
}

func TestDrawLine_Vertical(t *testing.T) {
	size := 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	lineColor := color.RGBA{R: 0, G: 255, B: 0, A: 255}
	drawLine(img, 32, 10, 32, 50, lineColor)

	for y := 10; y <= 50; y++ {
		got := img.RGBAAt(32, y)
		assert.Equal(t, lineColor, got, "pixel (32, %d) should be green", y)
	}
}

func TestDrawSmallDot(t *testing.T) {
	size := 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	dotColor := color.RGBA{R: 0, G: 0, B: 255, A: 255}
	cx, cy, r := 16, 16, 3
	drawSmallDot(img, cx, cy, r, dotColor)

	// Centre must be set
	assert.Equal(t, dotColor, img.RGBAAt(cx, cy))
	// Point exactly at radius boundary: r²=9, so (3,0): d²=9 ≤ 9 → filled
	assert.Equal(t, dotColor, img.RGBAAt(cx+r, cy))
	// Point just outside the circle: (cx+r+1, cy) d²=(r+1)² > r² → not filled
	outer := img.RGBAAt(cx+r+1, cy)
	assert.NotEqual(t, dotColor, outer)
}

func TestChooseBestZoom_WideArea(t *testing.T) {
	// Points 20 degrees apart → will not fit in 6×6 tiles at high zoom
	zoom := chooseBestZoom(20, 40, -120, -100)
	assert.LessOrEqual(t, zoom, 9)
}

func TestChooseBestZoom_TightArea(t *testing.T) {
	// Points only 0.001° apart → should comfortably fit at zoom 15
	zoom := chooseBestZoom(37.7749, 37.7750, -122.4194, -122.4193)
	assert.Equal(t, 15, zoom)
}

func TestChooseBestZoom_SinglePoint(t *testing.T) {
	// Same lat/lon → max zoom
	zoom := chooseBestZoom(37.7749, 37.7749, -122.4194, -122.4194)
	assert.Equal(t, 15, zoom)
}

func TestMapCacheDir_ReturnsNonEmpty(t *testing.T) {
	dir, err := mapCacheDir()
	require.NoError(t, err)
	assert.NotEmpty(t, dir)
}
