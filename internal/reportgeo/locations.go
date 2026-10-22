// Package reportgeo resolves explicit city names locally for aggregate report maps.
package reportgeo

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

//go:embed cities.tsv.gz
var cities []byte

type Point struct{ Latitude, Longitude float64 }

var index map[string]*Point
var once sync.Once

func normalize(s string) string {
	var out strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
		} else {
			out.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

func load() {
	index = map[string]*Point{}
	reader, err := gzip.NewReader(bytes.NewReader(cities))
	if err != nil {
		panic(err)
	} // Corrupt embedded build artifact, not user input.
	defer reader.Close()
	scan := bufio.NewScanner(reader)
	for scan.Scan() {
		f := strings.Split(scan.Text(), "\t")
		if len(f) != 8 {
			panic("invalid embedded city row")
		}
		lat, err := strconv.ParseFloat(f[6], 64)
		if err != nil {
			panic(err)
		}
		lon, err := strconv.ParseFloat(f[7], 64)
		if err != nil {
			panic(err)
		}
		p := &Point{lat, lon}
		add := func(s string) {
			key := normalize(s)
			if old, ok := index[key]; ok && old != p {
				index[key] = nil
			} else {
				index[key] = p
			}
		}
		add(f[0])
		countries := []string{f[1], f[2], f[3]}
		if f[2] == "GB" {
			countries = append(countries, "UK")
		}
		if f[2] == "US" {
			countries = append(countries, "United States of America")
		}
		for _, country := range countries {
			add(f[0] + " " + country)
			add(country + " " + f[0])
			if f[4] != "" {
				add(f[0] + " " + f[4] + " " + country)
				add(f[0] + " " + f[5] + " " + country)
			}
		}
		if f[4] != "" {
			add(f[0] + " " + f[4])
			add(f[0] + " " + f[5])
		}
	}
	if err := scan.Err(); err != nil {
		panic(err)
	}
}

// Lookup accepts only an exact, unambiguous city or city/country (US city/state)
// match after case, accent and punctuation normalization. No substring inference,
// country-to-capital substitution, or remote geocoding of personal form answers.
func Lookup(place string) (Point, bool) {
	once.Do(load)
	p := index[normalize(place)]
	if p == nil {
		return Point{}, false
	}
	return *p, true
}
