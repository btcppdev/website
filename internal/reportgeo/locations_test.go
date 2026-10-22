package reportgeo

import "testing"

func TestLookup(t *testing.T) {
	for _, name := range []string{"Nairobi, Kenya", "Lagos, Nigeria", "Abuja Nigeria", "Nairobi", "Berlin, Germany", "São Paulo, Brazil", "sao paulo BR", "Austin, TX, USA", "London, UK"} {
		p, ok := Lookup(name)
		if !ok || p.Latitude < -90 || p.Latitude > 90 || p.Longitude < -180 || p.Longitude > 180 {
			t.Errorf("unresolved %q: %+v", name, p)
		}
	}
	for _, name := range []string{"", "unknown", "Springfield", "Nigeria", "Lagos or Nairobi", "Nairobi / London", "Nairobi, Nigeria", "I may travel from Nairobi", "<script>alert(1)</script>"} {
		if _, ok := Lookup(name); ok {
			t.Errorf("ambiguous or invalid origin resolved: %q", name)
		}
	}
	p, ok := Lookup("Nairobi, Kenya")
	if !ok || p.Latitude > 0 || p.Longitude < 36 || p.Longitude > 38 {
		t.Fatal("incorrect Nairobi coordinates", p)
	}
}
