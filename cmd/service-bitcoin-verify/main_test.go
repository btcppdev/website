package main

import (
	"io"
	"testing"
)

func TestRequiresExplicitOutput(t *testing.T) {
	base := []string{"-review", "fixture", "-txid", "fixture", "-block", "fixture", "-destination", "fixture"}
	for _, extra := range [][]string{nil, {"-vout", "-1"}, {"-vout", "4294967296"}, {"-vout", "0", "unexpected"}} {
		if err := run(append(append([]string{}, base...), extra...), io.Discard); err == nil {
			t.Fatal("invalid output selection accepted")
		}
	}
}
