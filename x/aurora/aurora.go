package aurora

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

// DefaultApiURL is the NOAA OVATION latest forecast URL.
const DefaultApiURL = "https://services.swpc.noaa.gov/json/ovation_aurora_latest.json"

// OvationData represents the NOAA OVATION forecast data.
type OvationData struct {
	ForecastTime string  `json:"Forecast Time"`
	Coordinates  [][]int `json:"coordinates"`
}

// FetchOvationData fetches the latest OVATION aurora data from the given URL.
func FetchOvationData(url string) (*OvationData, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("aurora: failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aurora: failed to fetch data: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data OvationData
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("aurora: failed to parse json: %w", err)
	}

	return &data, nil
}

// FindProbability finds the aurora probability for a given latitude and longitude in the OVATION data.
func (data *OvationData) FindProbability(lat, lon float64) int {
	// Normalize longitude to 0-360 for OVATION data
	// NOAA OVATION data uses 0 to 360 degrees east.
	ovationLon := lon
	if ovationLon < 0 {
		ovationLon += 360
	}

	// Finding the nearest grid cell
	// The data is a grid: [longitude, latitude, aurora_probability]
	// Longitude is 0-359, Latitude is -90 to 90.

	bestProb := 0
	minDist := math.MaxFloat64

	for _, coord := range data.Coordinates {
		if len(coord) < 3 {
			continue
		}
		cLon := float64(coord[0])
		cLat := float64(coord[1])
		prob := coord[2]

		// Simple Euclidean distance for the grid (good enough for 1 degree grid)
		dLon := cLon - ovationLon
		if dLon > 180 {
			dLon -= 360
		} else if dLon < -180 {
			dLon += 360
		}
		dist := dLon*dLon + (cLat-lat)*(cLat-lat)

		if dist < minDist {
			minDist = dist
			bestProb = prob
		}
		// If we found an exact match (or very close), we can stop
		if dist < 0.1 {
			break
		}
	}

	return bestProb
}
