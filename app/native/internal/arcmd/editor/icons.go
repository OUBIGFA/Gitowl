package editor

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

// Icons are Solar Linear's (https://github.com/saoudi-h/solar-icons), MIT,
// artwork by 480 Design under CC BY 4.0. The files under icons/ keep the
// upstream geometry; only their black paint is currentColor. Sources, revision
// and checksums are registered in third_party/native-icons.json by
// tools/record_solar_icons.py.
//
//go:embed icons/*.svg
var iconFiles embed.FS

func solar(name string) *ui.SVG {
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

// Solar has no numbered-heading, sigma or wrap-text glyph. These stay
// Lucide's (https://lucide.dev, ISC; portions from Feather, MIT, Cole Bemis
// 2013-2022; Lucide Contributors 2022), drawn at Solar's 1.5 stroke.
func lucide(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

// Icons of the floating format bar and menus.
var (
	iconBold         = solar("text-bold")
	iconItalic       = solar("text-italic")
	iconUnderline    = solar("text-underline")
	iconStrike       = solar("text-cross")
	iconCode         = solar("code")
	iconLink         = solar("link")
	iconPlus         = solar("add")
	iconFormat       = solar("text-format")
	iconChevron      = solar("alt-arrow-down")
	iconChevronRight = solar("alt-arrow-right")
	iconChevronsLeft = solar("double-alt-arrow-left")
	iconOutline      = solar("list")
	iconAlignLeft    = solar("text-align-left")
	iconAlignCenter  = solar("text-align-center")
	iconAlignRight   = solar("text-align-right")
	// The read-only toggle.
	iconLocked   = solar("lock")
	iconUnlocked = solar("lock-unlocked")
)

// Icons drawn on the page.
var (
	iconGrip   = solar("grip-vertical")
	iconBulb   = solar("lightbulb")
	iconCheck  = solar("check")
	iconInfo   = solar("info-circle")
	iconNotice = solar("chat-square-warning")
	iconWarn   = solar("danger-triangle")
	iconStop   = solar("danger")
	iconCopy   = solar("copy")
)

// Icons of the content types, in the insert and slash menus.
var (
	iconParagraph = solar("paragraph")
	iconHeading1  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="m17 12 3-2v8"/>`)
	iconHeading2  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="M21 18h-4c0-4 4-3 4-6 0-1.5-2-2.5-4-1"/>`)
	iconHeading3  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="M17.5 10.5c1.7-1 3.5 0 3.5 1.5a2 2 0 0 1-2 2"/><path d="M17 17.5c2 1.5 4 .3 4-1.5a2 2 0 0 0-2-2"/>`)
	iconHeading4  = lucide(`<path d="M12 18V6"/><path d="M17 10v3a1 1 0 0 0 1 1h3"/><path d="M21 10v8"/><path d="M4 12h8"/><path d="M4 18V6"/>`)
	iconHeading5  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="M17 13v-3h4"/><path d="M17 17.7c.4.2.8.3 1.3.3 1.5 0 2.7-1.1 2.7-2.5S19.8 13 18.3 13H17"/>`)
	iconList      = solar("list-unordered")
	iconOrdered   = solar("list-ordered")
	iconTodo      = solar("list-check")
	iconQuote     = solar("quote")
	iconCodeBlock = solar("code-square")
	iconTable     = solar("table")
	iconColumns   = solar("columns-2")
	iconSortUp    = solar("sort-from-bottom-to-top")
	iconSortDown  = solar("sort-from-top-to-bottom")
	iconToggle    = solar("square-alt-arrow-right")
	iconRule      = solar("minus")
	iconImage     = solar("gallery")
)

// Icons of the block menu.
var (
	iconIndent       = solar("list-indent-increase")
	iconOutdent      = solar("list-indent-decrease")
	iconPalette      = solar("palette")
	iconScissors     = solar("scissors")
	iconDuplicate    = solar("copy-add")
	iconTrash        = solar("trash-bin-minimalistic")
	iconArrowUp      = solar("arrow-up")
	iconArrowDown    = solar("arrow-down")
	iconInsertLeft   = solar("arrow-left-to-line")
	iconInsertRight  = solar("arrow-right-to-line")
	iconInsertAbove  = solar("arrow-up-to-line")
	iconInsertBelow  = solar("arrow-down-to-line")
	iconSquarePlus   = solar("add-square")
	iconClose        = solar("close")
	iconPaintBucket  = solar("paint-bucket")
	iconMerge        = solar("table-cells-merge")
	iconSplit        = solar("table-cells-split")
	iconSlash        = solar("forbidden-circle")
	iconSmile        = solar("smile-circle")
	iconMoveVertical = solar("move-vertical")
	iconWorkflow     = solar("workflow")
	iconSequence     = solar("transfer-horizontal")
	iconPencil       = solar("pen")
	iconSigma        = lucide(`<path d="M18 7V5a1 1 0 0 0-1-1H6.5a.5.5 0 0 0-.4.8l4.5 6a2 2 0 0 1 0 2.4l-4.5 6a.5.5 0 0 0 .4.8H17a1 1 0 0 0 1-1v-2"/>`)
	iconWrapText     = lucide(`<path d="M3 6h18"/><path d="M3 12h15a3 3 0 1 1 0 6h-4"/><polyline points="16 16 14 18 16 20"/><path d="M3 18h7"/>`)
	// The fold disclosure is a filled glyph of ArcMD's own, not an icon set's.
	iconTriangleDown  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor"><path d="M6.5 9h11a.6.6 0 0 1 .45 1l-5.5 6.2a.6.6 0 0 1-.9 0L6.05 10a.6.6 0 0 1 .45-1z"/></svg>`))
	iconTriangleRight = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor"><path d="M9 6.5v11a.6.6 0 0 0 1 .45l6.2-5.5a.6.6 0 0 0 0-.9L10 6.05a.6.6 0 0 0-1 .45z"/></svg>`))
)
