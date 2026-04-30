// Command benchmark-resolve runs the corpus (testdata/korean_corpus.json)
// against a running kittypaw-api instance and reports precision.
//
// Usage:
//
//	make benchmark-resolve                            # default localhost:8080
//	go run ./cmd/benchmark-resolve --base http://localhost:8080
//
// The --gate flag is the merge gate (default 0.85). If precision falls
// below it, exit code 1.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"time"
)

type corpusItem struct {
	Q              string  `json:"q"`
	ExpectedStatus int     `json:"expected_status"`
	ExpectedLat    float64 `json:"expected_lat,omitempty"`
	ExpectedLon    float64 `json:"expected_lon,omitempty"`
	ToleranceM     float64 `json:"tolerance_m,omitempty"`
}

type corpusFile struct {
	TargetPrecision float64      `json:"target_precision"`
	Items           []corpusItem `json:"items"`
}

type resolveResp struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

func main() {
	var (
		base   = flag.String("base", "http://localhost:8080", "kittypaw-api base URL")
		corpus = flag.String("corpus", "testdata/korean_corpus.json", "corpus path")
		gate   = flag.Float64("gate", 0.85, "minimum precision")
	)
	flag.Parse()

	b, err := os.ReadFile(*corpus)
	if err != nil {
		log.Fatalf("read corpus: %v", err)
	}
	var c corpusFile
	if err := json.Unmarshal(b, &c); err != nil {
		log.Fatalf("parse corpus: %v", err)
	}
	if c.TargetPrecision > 0 {
		*gate = c.TargetPrecision
	}

	client := &http.Client{Timeout: 10 * time.Second}
	pass, fail := 0, 0
	for _, it := range c.Items {
		ok, msg := check(client, *base, it)
		if ok {
			pass++
		} else {
			fail++
			fmt.Printf("FAIL  q=%q  %s\n", it.Q, msg)
		}
	}

	total := pass + fail
	prec := float64(pass) / float64(total)
	fmt.Printf("\nprecision: %d/%d = %.3f (gate %.3f)\n", pass, total, prec, *gate)
	if prec < *gate {
		os.Exit(1)
	}
}

func check(client *http.Client, base string, it corpusItem) (bool, string) {
	u := base + "/v1/geo/resolve?q=" + url.QueryEscape(it.Q)
	resp, err := client.Get(u)
	if err != nil {
		return false, fmt.Sprintf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode != it.ExpectedStatus {
		return false, fmt.Sprintf("status %d, want %d (%s)", resp.StatusCode, it.ExpectedStatus, body)
	}

	if it.ExpectedStatus != 200 {
		return true, ""
	}

	// On 200, optionally check coordinates.
	if it.ExpectedLat == 0 && it.ExpectedLon == 0 {
		return true, ""
	}
	var rr resolveResp
	if err := json.Unmarshal(body, &rr); err != nil {
		return false, fmt.Sprintf("decode: %v", err)
	}
	dist := haversineMeters(rr.Lat, rr.Lon, it.ExpectedLat, it.ExpectedLon)
	if it.ToleranceM > 0 && dist > it.ToleranceM {
		return false, fmt.Sprintf("dist %.0fm > tolerance %.0fm (got %.4f,%.4f want %.4f,%.4f)",
			dist, it.ToleranceM, rr.Lat, rr.Lon, it.ExpectedLat, it.ExpectedLon)
	}
	return true, ""
}

// haversineMeters returns great-circle distance between two WGS84 points.
func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}
