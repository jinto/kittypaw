package kma

import (
	"errors"
	"math"
)

// LCC projection constants from KMA 동네예보 격자좌표체계.
const (
	gridRE      = 6371.00877 // earth radius (km)
	gridSpacing = 5.0        // grid spacing (km)
	gridSlat1   = 30.0       // standard latitude 1 (deg)
	gridSlat2   = 60.0       // standard latitude 2 (deg)
	gridOlon    = 126.0      // origin longitude (deg)
	gridOlat    = 38.0       // origin latitude (deg)
	gridXo      = 43         // origin x grid
	gridYo      = 136        // origin y grid

	// Korean peninsula bounding box. KMA only serves data within this range;
	// coordinates outside cause upstream NO_DATA, so we reject early.
	minLat = 33.0
	maxLat = 39.0
	minLon = 124.0
	maxLon = 132.0
)

// ErrOutOfKoreaPeninsula is returned when lat/lon falls outside the KMA
// service area. Callers map this to a 400 response.
var ErrOutOfKoreaPeninsula = errors.New("coordinate out of Korea peninsula")

// LatLngToGrid converts WGS84 lat/lon to the KMA forecast grid (nx, ny)
// using the published Lambert Conformal Conic projection.
func LatLngToGrid(lat, lon float64) (nx, ny int, err error) {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return 0, 0, ErrOutOfKoreaPeninsula
	}
	if lat < minLat || lat > maxLat || lon < minLon || lon > maxLon {
		return 0, 0, ErrOutOfKoreaPeninsula
	}

	const degRad = math.Pi / 180.0
	re := gridRE / gridSpacing
	slat1 := gridSlat1 * degRad
	slat2 := gridSlat2 * degRad
	olon := gridOlon * degRad
	olat := gridOlat * degRad

	sn := math.Tan(math.Pi*0.25+slat2*0.5) / math.Tan(math.Pi*0.25+slat1*0.5)
	sn = math.Log(math.Cos(slat1)/math.Cos(slat2)) / math.Log(sn)
	sf := math.Tan(math.Pi*0.25 + slat1*0.5)
	sf = math.Pow(sf, sn) * math.Cos(slat1) / sn
	ro := math.Tan(math.Pi*0.25 + olat*0.5)
	ro = re * sf / math.Pow(ro, sn)

	ra := math.Tan(math.Pi*0.25 + lat*degRad*0.5)
	ra = re * sf / math.Pow(ra, sn)
	theta := lon*degRad - olon
	if theta > math.Pi {
		theta -= 2 * math.Pi
	}
	if theta < -math.Pi {
		theta += 2 * math.Pi
	}
	theta *= sn

	nx = int(math.Floor(ra*math.Sin(theta) + float64(gridXo) + 0.5))
	ny = int(math.Floor(ro - ra*math.Cos(theta) + float64(gridYo) + 0.5))
	return nx, ny, nil
}
