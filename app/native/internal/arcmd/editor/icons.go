package editor

import "github.com/egoist/mygo/ui"

// Icons are Lucide's (https://lucide.dev), ISC License,
// Copyright (c) for portions of Lucide are held by Cole Bemis 2013-2022 as
// part of Feather (MIT). All other copyright (c) for Lucide are held by
// Lucide Contributors 2022.

func lucide(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

// Icons of the floating format bar and menus.
var (
	iconBold         = lucide(`<path d="M6 12h9a4 4 0 0 1 0 8H7a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1h7a4 4 0 0 1 0 8"/>`)
	iconItalic       = lucide(`<path d="M19 4h-9"/><path d="M14 20H5"/><path d="M15 4 9 20"/>`)
	iconUnderline    = lucide(`<path d="M6 4v6a6 6 0 0 0 12 0V4"/><path d="M4 20h16"/>`)
	iconStrike       = lucide(`<path d="M16 4H9a3 3 0 0 0-2.83 4"/><path d="M14 12a4 4 0 0 1 0 8H6"/><path d="M4 12h16"/>`)
	iconCode         = lucide(`<path d="m16 18 6-6-6-6"/><path d="m8 6-6 6 6 6"/>`)
	iconLink         = lucide(`<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>`)
	iconPlus         = lucide(`<path d="M5 12h14"/><path d="M12 5v14"/>`)
	iconFormat       = lucide(`<polyline points="4 7 4 4 20 4 20 7"/><line x1="9" x2="15" y1="20" y2="20"/><line x1="12" x2="12" y1="4" y2="20"/>`)
	iconChevron      = lucide(`<path d="m6 9 6 6 6-6"/>`)
	iconChevronRight = lucide(`<path d="m9 18 6-6-6-6"/>`)
	iconChevronsLeft = lucide(`<path d="m11 17-5-5 5-5"/><path d="m18 17-5-5 5-5"/>`)
	iconOutline      = lucide(`<path d="M21 5H3"/><path d="M15 12H3"/><path d="M17 19H3"/>`)
	iconAlignLeft    = lucide(`<path d="M21 5H3"/><path d="M15 12H3"/><path d="M17 19H3"/>`)
	iconAlignCenter  = lucide(`<path d="M21 5H3"/><path d="M17 12H7"/><path d="M19 19H5"/>`)
	// The read-only toggle: Lucide's lock and lock-open.
	iconLocked     = lucide(`<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>`)
	iconUnlocked   = lucide(`<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 9.9-1"/>`)
	iconAlignRight = lucide(`<path d="M21 5H3"/><path d="M21 12H9"/><path d="M21 19H7"/>`)
)

// Icons drawn on the page.
var (
	iconGrip   = lucide(`<circle cx="9" cy="12" r="1"/><circle cx="9" cy="5" r="1"/><circle cx="9" cy="19" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="15" cy="5" r="1"/><circle cx="15" cy="19" r="1"/>`)
	iconBulb   = lucide(`<path d="M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5"/><path d="M9 18h6"/><path d="M10 22h4"/>`)
	iconCheck  = lucide(`<path d="M20 6 9 17l-5-5"/>`)
	iconInfo   = lucide(`<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>`)
	iconNotice = lucide(`<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/><path d="M12 7v2"/><path d="M12 13h.01"/>`)
	iconWarn   = lucide(`<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/>`)
	iconStop   = lucide(`<path d="M12 16h.01"/><path d="M12 8v4"/><path d="M15.312 2a2 2 0 0 1 1.414.586l4.688 4.688A2 2 0 0 1 22 8.688v6.624a2 2 0 0 1-.586 1.414l-4.688 4.688a2 2 0 0 1-1.414.586H8.688a2 2 0 0 1-1.414-.586l-4.688-4.688A2 2 0 0 1 2 15.312V8.688a2 2 0 0 1 .586-1.414l4.688-4.688A2 2 0 0 1 8.688 2z"/>`)
	iconCopy   = lucide(`<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>`)
)

// Icons of the content types, in the insert and slash menus.
var (
	iconParagraph = lucide(`<path d="M13 4v16"/><path d="M17 4v16"/><path d="M19 4H9.5a4.5 4.5 0 0 0 0 9H13"/>`)
	iconHeading1  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="m17 12 3-2v8"/>`)
	iconHeading2  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="M21 18h-4c0-4 4-3 4-6 0-1.5-2-2.5-4-1"/>`)
	iconHeading3  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="M17.5 10.5c1.7-1 3.5 0 3.5 1.5a2 2 0 0 1-2 2"/><path d="M17 17.5c2 1.5 4 .3 4-1.5a2 2 0 0 0-2-2"/>`)
	iconList      = lucide(`<path d="M3 12h.01"/><path d="M3 18h.01"/><path d="M3 6h.01"/><path d="M8 12h13"/><path d="M8 18h13"/><path d="M8 6h13"/>`)
	iconOrdered   = lucide(`<path d="M10 12h11"/><path d="M10 18h11"/><path d="M10 6h11"/><path d="M4 10h2"/><path d="M4 6h1v4"/><path d="M6 18H4c0-1 2-2 2-3s-1-1.5-2-1"/>`)
	iconTodo      = lucide(`<rect x="3" y="5" width="6" height="6" rx="1"/><path d="m3 17 2 2 4-4"/><path d="M13 6h8"/><path d="M13 12h8"/><path d="M13 18h8"/>`)
	iconQuote     = lucide(`<path d="M17 6H3"/><path d="M21 12H8"/><path d="M21 18H8"/><path d="M3 12v6"/>`)
	iconCodeBlock = lucide(`<path d="m10 9-3 3 3 3"/><path d="m14 15 3-3-3-3"/><rect x="3" y="3" width="18" height="18" rx="2"/>`)
	iconTable     = lucide(`<path d="M12 3v18"/><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M3 9h18"/><path d="M3 15h18"/>`)
	iconColumns   = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M12 3v18"/>`)
	iconSortUp    = lucide(`<path d="m3 8 4-4 4 4"/><path d="M7 4v16"/><path d="M20 8h-5"/><path d="M15 10V6.5a2.5 2.5 0 0 1 5 0V10"/><path d="M15 14h5l-5 6h5"/>`)
	iconSortDown  = lucide(`<path d="m3 16 4 4 4-4"/><path d="M7 20V4"/><path d="M15 4h5l-5 6h5"/><path d="M15 20v-3.5a2.5 2.5 0 0 1 5 0V20"/><path d="M20 18h-5"/>`)
	iconToggle    = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="m10 8 4 4-4 4"/>`)
	iconRule      = lucide(`<path d="M5 12h14"/>`)
	iconHeading4  = lucide(`<path d="M12 18V6"/><path d="M17 10v3a1 1 0 0 0 1 1h3"/><path d="M21 10v8"/><path d="M4 12h8"/><path d="M4 18V6"/>`)
	iconHeading5  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="M17 13v-3h4"/><path d="M17 17.7c.4.2.8.3 1.3.3 1.5 0 2.7-1.1 2.7-2.5S19.8 13 18.3 13H17"/>`)
	iconImage     = lucide(`<rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/>`)
)

// Icons of the block menu.
var (
	iconIndent        = lucide(`<path d="M21 5H11"/><path d="M21 12H11"/><path d="M21 19H11"/><path d="m3 8 4 4-4 4"/>`)
	iconOutdent       = lucide(`<path d="M21 5H11"/><path d="M21 12H11"/><path d="M21 19H11"/><path d="m7 8-4 4 4 4"/>`)
	iconPalette       = lucide(`<path d="M12 22a1 1 0 0 1 0-20 10 9 0 0 1 10 9 5 5 0 0 1-5 5h-2.25a1.75 1.75 0 0 0-1.4 2.8l.3.4a1.75 1.75 0 0 1-1.4 2.8z"/><circle cx="13.5" cy="6.5" r=".5" fill="currentColor"/><circle cx="17.5" cy="10.5" r=".5" fill="currentColor"/><circle cx="6.5" cy="12.5" r=".5" fill="currentColor"/><circle cx="8.5" cy="7.5" r=".5" fill="currentColor"/>`)
	iconScissors      = lucide(`<circle cx="6" cy="6" r="3"/><path d="M8.12 8.12 12 12"/><path d="M20 4 8.12 15.88"/><circle cx="6" cy="18" r="3"/><path d="M14.8 14.8 20 20"/>`)
	iconDuplicate     = lucide(`<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/><path d="M15 12v6"/><path d="M12 15h6"/>`)
	iconTrash         = lucide(`<path d="M10 11v6"/><path d="M14 11v6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M3 6h18"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>`)
	iconArrowUp       = lucide(`<path d="m5 12 7-7 7 7"/><path d="M12 19V5"/>`)
	iconArrowDown     = lucide(`<path d="M12 5v14"/><path d="m19 12-7 7-7-7"/>`)
	iconInsertLeft    = lucide(`<path d="M3 19V5"/><path d="m13 6-6 6 6 6"/><path d="M7 12h14"/>`)
	iconInsertRight   = lucide(`<path d="M17 12H3"/><path d="m11 18 6-6-6-6"/><path d="M21 5v14"/>`)
	iconInsertAbove   = lucide(`<path d="M5 3h14"/><path d="m18 13-6-6-6 6"/><path d="M12 7v14"/>`)
	iconInsertBelow   = lucide(`<path d="M12 17V3"/><path d="m6 11 6 6 6-6"/><path d="M19 21H5"/>`)
	iconSquarePlus    = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M8 12h8"/><path d="M12 8v8"/>`)
	iconTriangleDown  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor"><path d="M6.5 9h11a.6.6 0 0 1 .45 1l-5.5 6.2a.6.6 0 0 1-.9 0L6.05 10a.6.6 0 0 1 .45-1z"/></svg>`))
	iconTriangleRight = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor"><path d="M9 6.5v11a.6.6 0 0 0 1 .45l6.2-5.5a.6.6 0 0 0 0-.9L10 6.05a.6.6 0 0 0-1 .45z"/></svg>`))
	iconClose         = lucide(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
	iconPaintBucket   = lucide(`<path d="m19 11-8-8-8.6 8.6a2 2 0 0 0 0 2.8l5.2 5.2c.8.8 2 .8 2.8 0L19 11Z"/><path d="m5 2 5 5"/><path d="M2 13h15"/><path d="M22 20a2 2 0 1 1-4 0c0-1.6 1.7-2.4 2-4 .3 1.6 2 2.4 2 4Z"/>`)
	iconMerge         = lucide(`<path d="M8 19H5c-1 0-2-1-2-2V7c0-1 1-2 2-2h3"/><path d="M16 5h3c1 0 2 1 2 2v10c0 1-1 2-2 2h-3"/><path d="M12 4v16"/><path d="m7 15 3-3-3-3"/><path d="m17 9-3 3 3 3"/>`)
	iconSplit         = lucide(`<path d="M8 19H5c-1 0-2-1-2-2V7c0-1 1-2 2-2h3"/><path d="M16 5h3c1 0 2 1 2 2v10c0 1-1 2-2 2h-3"/><path d="M12 4v16"/><path d="m10 15-3-3 3-3"/><path d="m14 9 3 3-3 3"/>`)
	iconSlash         = lucide(`<path d="M4 20 20 4"/>`)
	iconSmile         = lucide(`<circle cx="12" cy="12" r="10"/><path d="M8 14s1.5 2 4 2 4-2 4-2"/><line x1="9" x2="9.01" y1="9" y2="9"/><line x1="15" x2="15.01" y1="9" y2="9"/>`)
	iconMoveVertical  = lucide(`<path d="M12 2v20"/><path d="m8 18 4 4 4-4"/><path d="m8 6 4-4 4 4"/>`)
	iconSigma         = lucide(`<path d="M18 7V5a1 1 0 0 0-1-1H6.5a.5.5 0 0 0-.4.8l4.5 6a2 2 0 0 1 0 2.4l-4.5 6a.5.5 0 0 0 .4.8H17a1 1 0 0 0 1-1v-2"/>`)
	iconWrapText      = lucide(`<path d="M3 6h18"/><path d="M3 12h15a3 3 0 1 1 0 6h-4"/><polyline points="16 16 14 18 16 20"/><path d="M3 18h7"/>`)
	iconWorkflow      = lucide(`<rect width="8" height="8" x="3" y="3" rx="2"/><path d="M7 11v4a2 2 0 0 0 2 2h4"/><rect width="8" height="8" x="13" y="13" rx="2"/>`)
	iconSequence      = lucide(`<path d="M6 4v16"/><path d="M18 4v16"/><path d="M6 9h12"/><path d="m14 6 4 3-4 3"/><path d="M18 15H6"/><path d="m10 12-4 3 4 3"/>`)
	iconPencil        = lucide(`<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/>`)
)
