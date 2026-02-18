package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"keyop/core"
	"os"
	"time"

	"github.com/mcuadros/go-rpi-rgb-led-matrix"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type RGBMatrixPlugin struct {
	deps           core.Dependencies
	cfg            core.ServiceConfig
	matrix         rgbmatrix.Matrix
	canvas         *rgbmatrix.Canvas
	timeFace       font.Face
	dayOfWeekFace  font.Face
	dayOfMonthFace font.Face
	swapGB         bool
}

func (p *RGBMatrixPlugin) Initialize() error {
	p.deps.MustGetLogger().Info("RGBMatrixPlugin initializing")

	config := &rgbmatrix.DefaultConfig
	config.Rows = 32
	config.Cols = 64
	config.Parallel = 1
	config.ChainLength = 1
	config.Brightness = 50
	config.HardwareMapping = "adafruit-hat"
	config.ShowRefreshRate = false
	config.InverseColors = false
	config.DisableHardwarePulsing = false

	// Allow overrides from config
	if v, ok := p.cfg.Config["rows"].(float64); ok {
		config.Rows = int(v)
	}
	if v, ok := p.cfg.Config["cols"].(float64); ok {
		config.Cols = int(v)
	}
	if v, ok := p.cfg.Config["swap_gb"].(bool); ok {
		p.swapGB = v
	}

	m, err := rgbmatrix.NewRGBLedMatrix(config)
	if err != nil {
		return fmt.Errorf("failed to initialize matrix: %w", err)
	}

	p.matrix = m
	p.canvas = rgbmatrix.NewCanvas(p.matrix)

	p.timeFace, err = loadFontFace("resources/pixelmix-8.ttf", 8)
	if err != nil {
		return err
	}

	p.dayOfMonthFace, err = loadFontFace("resources/pixelation-7.ttf", 7)
	if err != nil {
		return err
	}

	p.dayOfWeekFace, err = loadFontFace("resources/pixel-letters-6.ttf", 6)
	if err != nil {
		return err
	}

	err = p.canvas.Clear()
	if err != nil {
		return fmt.Errorf("failed to clear canvas: %w", err)
	}

	p.deps.MustGetLogger().Info("RGBMatrixPlugin initialized", "rows", config.Rows, "cols", config.Cols)
	return nil
}

func loadFontFace(path string, size float64) (font.Face, error) {
	fontBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read font file %s: %w", path, err)
	}
	f, err := opentype.Parse(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse font %s: %w", path, err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create font face from %s: %w", path, err)
	}
	return face, nil
}

func (p *RGBMatrixPlugin) colorRGBA(r, g, b, a uint8) color.RGBA {
	if p.swapGB {
		return color.RGBA{r, b, g, a}
	}
	return color.RGBA{r, g, b, a}
}

func (p *RGBMatrixPlugin) Check() error {
	logger := p.deps.MustGetLogger()
	logger.Warn("RGBMatrixPlugin Check called")

	ctx := p.deps.MustGetContext()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Initial render
	if err := p.Render(); err != nil {
		logger.Error("Failed to render", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("RGBMatrixPlugin Check context cancelled")
			return ctx.Err()
		case <-ticker.C:
			if err := p.Render(); err != nil {
				logger.Error("Failed to render", "error", err)
			}
		}
	}
}

func (p *RGBMatrixPlugin) Render() error {
	logger := p.deps.MustGetLogger()
	logger.Warn("RGBMatrixPlugin Render called")

	if p.canvas == nil {
		return fmt.Errorf("canvas not initialized")
	}

	logger.Warn("Foo: RGBMatrixPlugin Check passed")

	t := time.Now()
	timeStr := t.Format("3:04pm")
	dayOfWeek := t.Format("Mon")
	dayOfMonth := t.Format("_2") // Day of month (1-31)

	p.deps.MustGetLogger().Debug("RGBMatrixPlugin displaying info", "time", timeStr, "day", dayOfWeek, "date", dayOfMonth)

	// Create an image to draw the text onto
	bounds := p.canvas.Bounds()
	img := image.NewRGBA(bounds)

	// Draw text to the image
	d := &font.Drawer{
		Dst: img,
		Src: image.NewUniform(p.colorRGBA(100, 0, 255, 255)),
	}

	// 1. Draw Day of Week (Upper Left)
	d.Face = p.dayOfWeekFace
	d.Dot = fixed.Point26_6{X: fixed.I(0), Y: fixed.I(6)}
	d.DrawString(dayOfWeek)

	// 2. Draw Day of Month (Upper Left, Below Day of Week)
	d.Face = p.dayOfMonthFace
	d.Dot = fixed.Point26_6{X: fixed.I(0), Y: fixed.I(11)}
	d.DrawString(dayOfMonth)

	// 3. Draw Time (Top Center/Right)
	d.Face = p.timeFace
	d.Dot = fixed.Point26_6{X: fixed.I(20), Y: fixed.I(10)}
	d.DrawString(timeStr)

	// Copy image pixels to canvas
	draw.Draw(p.canvas, bounds, img, image.Point{}, draw.Over)

	p.canvas.Render()
	logger.Warn("RGBMatrixPlugin Render called")

	return nil
}

func (p *RGBMatrixPlugin) ValidateConfig() []error {
	return nil
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &RGBMatrixPlugin{
		deps: deps,
		cfg:  cfg,
	}
}
