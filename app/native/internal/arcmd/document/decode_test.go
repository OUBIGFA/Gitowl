package document

import (
	"fmt"
	"testing"
)

func TestDecodeRejectsMalformedTableCells(t *testing.T) {
	for version := 1; version <= CurrentVersion; version++ {
		for _, cells := range []string{
			`null`,
			`{"id":"a","kind":"cell"},null`,
			`{"id":"a","kind":"cell","colspan":2},null`,
			`{"id":"a","kind":"paragraph"}`,
			`{"id":"a","kind":"cell","colspan":9223372036854775807}`,
		} {
			t.Run(fmt.Sprintf("v%d/%s", version, cells), func(t *testing.T) {
				text := fmt.Sprintf(`{"format":"arcmd","version":%d,"blocks":[{"id":"table","kind":"table","children":[{"id":"row","kind":"row","children":[%s]}]}]}`, version, cells)
				if _, err := Decode(text); err == nil {
					t.Fatal("accepted a malformed table")
				}
			})
		}
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(`{"format":"arcmd","version":6,"blocks":[{"id":"p","kind":"paragraph"}]}`)
	f.Add(`{"format":"arcmd","version":6,"blocks":[{"id":"t","kind":"table","children":[{"id":"r","kind":"row","children":[null]}]}]}`)
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 1<<20 {
			t.Skip("input exceeds the fuzz corpus size limit")
		}
		doc, err := Decode(text)
		if err != nil {
			return
		}
		encoded, err := doc.Encode()
		if err != nil {
			t.Fatal("decoded document cannot be encoded", err)
		}
		again, err := Decode(encoded)
		if err != nil {
			t.Fatal("encoded document cannot be decoded", err)
		}
		if roundTrip, err := again.Encode(); err != nil || roundTrip != encoded {
			t.Fatal("native round trip changed the document", err)
		}
	})
}
