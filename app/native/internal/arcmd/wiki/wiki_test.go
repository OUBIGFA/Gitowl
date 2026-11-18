package wiki

import (
	"slices"
	"testing"
)

func TestLinks(t *testing.T) {
	text := "See [[Trip Plan]] and [[Ideas|my ideas]], [[Notes#Today]]; not [[]] or [[a\nb]]. [[x [[Inner]]"
	var got []string
	for _, l := range Links(text) {
		got = append(got, l.Target+"="+l.Label+"="+text[l.Start:l.End])
	}
	want := []string{
		"Trip Plan=Trip Plan=[[Trip Plan]]",
		"Ideas=my ideas=[[Ideas|my ideas]]",
		"Notes=Notes=[[Notes#Today]]",
		"Inner=Inner=[[Inner]]",
	}
	if !slices.Equal(got, want) {
		t.Errorf("links = %q, want %q", got, want)
	}
	if !LinksTo(text, "trip plan ") || LinksTo(text, "Trip") {
		t.Error("LinksTo")
	}
}

func TestObsidianEmbedsIgnoredByWikiLinks(t *testing.T) {
	text := "![[Pasted image 20261027110938.png]]\n![[chart.png|360]] and see [[Trip Plan]]"
	var got []string
	for _, l := range Links(text) {
		got = append(got, l.Target)
	}
	if !slices.Equal(got, []string{"Trip Plan"}) {
		t.Fatalf("Links = %q, want [Trip Plan]", got)
	}
	if LinksTo(text, "Pasted image 20261027110938.png") || LinksTo(text, "chart.png") {
		t.Fatal("embed matched LinksTo")
	}
	if renamed, ok := Rename("![[chart.png]] and [[chart.png]]", "chart.png", "diagram.png"); !ok || renamed != "![[chart.png]] and [[diagram.png]]" {
		t.Fatalf("Rename altered embed: %q (ok=%v)", renamed, ok)
	}
	if _, _, ok := Open("![[Pasted"); ok {
		t.Fatal("Open triggered on Obsidian embed prefix ![[")
	}
}

func TestRename(t *testing.T) {
	got, ok := Rename("[[Old]], [[old|alias]], [[Old#h]] and [[Older]]", "Old", "New")
	if !ok || got != "[[New]], [[New|alias]], [[New#h]] and [[Older]]" {
		t.Errorf("rename = %q %v", got, ok)
	}
	if _, ok := Rename("[[Other]]", "Old", "New"); ok {
		t.Error("renamed a link to another document")
	}
}

func TestTags(t *testing.T) {
	text := "#idea at start, (#草稿) and #work/2026 but not C# a#b #1 http://x/#frag #end-。#旅行"
	var got []string
	for _, tag := range Tags(text) {
		got = append(got, tag.Name+"="+text[tag.Start:tag.End])
	}
	want := []string{"idea=#idea", "草稿=#草稿", "work/2026=#work/2026", "end=#end", "旅行=#旅行"}
	if !slices.Equal(got, want) {
		t.Errorf("tags = %q, want %q", got, want)
	}
	if names := TagNames("#a #B #b #c"); !slices.Equal(names, []string{"a", "B", "c"}) {
		t.Errorf("names = %q", names)
	}
}

func TestOpen(t *testing.T) {
	if at, typed, ok := Open("see [[Pro"); !ok || at != 4 || typed != "Pro" {
		t.Errorf("open = %d %q %v", at, typed, ok)
	}
	if _, _, ok := Open("see [[Done]] after"); ok {
		t.Error("a finished link is open")
	}
	if _, typed, ok := Open("[["); !ok || typed != "" {
		t.Error("just the brackets")
	}
}

func TestRenameTag(t *testing.T) {
	got, ok := RenameTag("去 #工作 和 #工作/2026，不是 #工作室 也不是 C#工作", "工作", "职场")
	if !ok || got != "去 #职场 和 #职场/2026，不是 #工作室 也不是 C#工作" {
		t.Fatal(got)
	}
	if _, ok := RenameTag("#Go", "go", ""); ok {
		t.Fatal("no empty name")
	}
	if got, _ := RenameTag("#Go #go", "GO", "golang"); got != "#golang #golang" {
		t.Fatal("ignores case", got)
	}
	if !ValidTag("旅行/2026") || ValidTag("两 个") || ValidTag("123") || ValidTag("") {
		t.Fatal("valid tags")
	}
}
