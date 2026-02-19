package aurora

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFetchOvationData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := OvationData{
			ForecastTime: "2026-02-18T21:00:00Z",
			Coordinates: [][]int{
				{267, 45, 10},
				{0, 0, 0},
			},
		}
		json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()

	data, err := FetchOvationData(server.URL)
	assert.NoError(t, err)
	assert.Equal(t, "2026-02-18T21:00:00Z", data.ForecastTime)
	assert.Len(t, data.Coordinates, 2)
}

func TestOvationData_FindProbability(t *testing.T) {
	data := &OvationData{
		ForecastTime: "2026-02-18T21:00:00Z",
		Coordinates: [][]int{
			{10, 50, 20},  // 10E, 50N, 20%
			{350, 50, 30}, // 350E (10W), 50N, 30%
			{180, 0, 40},  // 180E, 0N, 40%
		},
	}

	tests := []struct {
		name         string
		lat, lon     float64
		expectedProb int
	}{
		{
			name:         "Near 10E, 50N",
			lat:          50.1,
			lon:          10.1,
			expectedProb: 20,
		},
		{
			name:         "Near 10W (350E), 50N",
			lat:          49.9,
			lon:          -9.9,
			expectedProb: 30,
		},
		{
			name:         "Near 180E, 0N",
			lat:          0.5,
			lon:          179.5,
			expectedProb: 40,
		},
		{
			name:         "Exactly at 10E, 50N",
			lat:          50.0,
			lon:          10.0,
			expectedProb: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prob := data.FindProbability(tt.lat, tt.lon)
			assert.Equal(t, tt.expectedProb, prob)
		})
	}
}

func TestFetchOvationData_Errors(t *testing.T) {
	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		_, err := FetchOvationData(server.URL)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "status 500")
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("invalid-json"))
		}))
		defer server.Close()

		_, err := FetchOvationData(server.URL)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse json")
	})
}
