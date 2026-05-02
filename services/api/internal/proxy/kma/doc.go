// Package kma provides utilities for the Korean Meteorological Administration
// (KMA) public APIs:
//
//   - LCC (Lambert Conformal Conic) lat/lon → grid (nx, ny) conversion
//   - Forecast publication base_time slot mapping
//
// Coefficients and slot schedule follow the KMA "동네예보 격자좌표체계" /
// "단기예보 발표시각" specifications.
package kma
