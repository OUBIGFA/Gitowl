package editor

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"unicode"
)

func referenceFindIn(text, query string) [][2]int {
	fold := func(r rune) rune { return unicode.ToLower(r) }
	q := []rune(strings.Map(fold, query))
	if len(q) == 0 {
		return nil
	}
	runes := []rune(text)
	var out [][2]int
	// offsets[i] is the UTF-16 offset of runes[i].
	offsets := make([]int, len(runes)+1)
	for i, r := range runes {
		n := 1
		if r > 0xffff {
			n = 2
		}
		offsets[i+1] = offsets[i] + n
	}
	for i := 0; i+len(q) <= len(runes); {
		ok := true
		for j, r := range q {
			if fold(runes[i+j]) != r {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, [2]int{offsets[i], offsets[i+len(q)]})
			i += len(q)
			continue
		}
		i++
	}
	return out
}

func TestLinearFindMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(141))
	alphabet := []rune("aAbB中😀KkİıΣσς\ufffd")
	for range 800 {
		text := make([]rune, rng.Intn(150))
		for i := range text {
			text[i] = alphabet[rng.Intn(len(alphabet))]
		}
		query := make([]rune, rng.Intn(8))
		for i := range query {
			query[i] = alphabet[rng.Intn(len(alphabet))]
		}
		a, b := findIn(string(text), string(query)), referenceFindIn(string(text), string(query))
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("%q / %q: %v vs %v", text, query, a, b)
		}
	}
	text := strings.Repeat("a", 100000) + "b"
	if got := findIn(text, strings.Repeat("a", 10000)+"b"); fmt.Sprint(got) != "[[90000 100001]]" {
		t.Fatal(got)
	}
}
func TestIncrementalFindMatchesReference(t *testing.T) {
	e := New("aAa😀aa\n\naba aa\n\n|aa|A|\n|---|---|\n|a|AA|")
	e.FindText("aa")
	rng := rand.New(rand.NewSource(142))
	check := func() {
		t.Helper()
		e.refreshMatches()
		var want []ui.TextInputRange
		for _, leaf := range e.Doc.Leaves() {
			for _, m := range referenceFindIn(leaf.Block.Text(), e.find.query) {
				want = append(want, ui.TextInputRange{Start: leaf.Start + m[0], End: leaf.Start + m[1]})
			}
		}
		if fmt.Sprint(e.find.matches) != fmt.Sprint(want) {
			t.Fatalf("find %q: %v / %v", e.find.query, e.find.matches, want)
		}
	}
	for range 250 {
		leaves := e.Doc.Leaves()
		leaf := leaves[rng.Intn(len(leaves))]
		switch rng.Intn(7) {
		case 0:
			e.Undo()
		case 1:
			e.Redo()
		case 2:
			e.find.query = []string{"aa", "a", "中", "😀", ""}[rng.Intn(5)]
		default:
			a, z := leaf.Start+rng.Intn(leaf.End-leaf.Start+1), leaf.Start+rng.Intn(leaf.End-leaf.Start+1)
			e.Engine.Replace(a, z, []string{"aaa", "中文😀", "x\ny", ""}[rng.Intn(4)])
		}
		check()
	}
}
