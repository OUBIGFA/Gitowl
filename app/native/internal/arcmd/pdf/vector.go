package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"sort"
	"strings"
)

// OutlineItem describes a hierarchical bookmark in the exported PDF outline tree.
type OutlineItem struct {
	Title string
	Page  int
	Y     float64 // Top-down Y in PDF points
	Level int
}

// LinkAnnot describes a clickable hyperlink or intra-document destination.
type LinkAnnot struct {
	X, Y, W, H float64
	URI        string // External URL when non-empty
	TargetPage int    // 0-based page index when URI == ""
	TargetY    float64
}

type vectorImageObj struct {
	alias string
	objID int
}

// VectorPage records scalable paths, filled shapes, Type 3 vector text,
// links, and raster images in top-down PDF points before flushing to disk.
type VectorPage struct {
	writer    *Writer
	pageIdx   int
	objID     int
	Width     float64
	Height    float64
	stream    bytes.Buffer
	fonts     map[string]int
	gsByAlpha map[uint16]string
	images    []vectorImageObj
	links     []LinkAnnot
	nextImg   int
}

// NewVectorPage begins a vector PDF page (using page index len(w.pages)).
func (w *Writer) NewVectorPage(width, height float64) *VectorPage {
	idx := len(w.pages)
	id := w.pageObjID(idx)
	return &VectorPage{
		writer:    w,
		pageIdx:   idx,
		objID:     id,
		Width:     width,
		Height:    height,
		fonts:     make(map[string]int),
		gsByAlpha: make(map[uint16]string),
	}
}

func (vp *VectorPage) py(y float64) float64 { return vp.Height - y }

func (vp *VectorPage) setAlpha(a uint8) {
	if a == 255 {
		vp.stream.WriteString("/GS_O gs ")
		vp.gsByAlpha[255] = "/GS_O"
		return
	}
	name, ok := vp.gsByAlpha[uint16(a)]
	if !ok {
		name = fmt.Sprintf("/GS%d", a)
		vp.gsByAlpha[uint16(a)] = name
	}
	vp.stream.WriteString(name + " gs ")
}

func (vp *VectorPage) setFillRGB(c [4]uint8) {
	vp.setAlpha(c[3])
	fmt.Fprintf(&vp.stream, "%.4f %.4f %.4f rg\n", float64(c[0])/255, float64(c[1])/255, float64(c[2])/255)
}

func (vp *VectorPage) setStrokeRGB(c [4]uint8) {
	vp.setAlpha(c[3])
	fmt.Fprintf(&vp.stream, "%.4f %.4f %.4f RG\n", float64(c[0])/255, float64(c[1])/255, float64(c[2])/255)
}

func (vp *VectorPage) Save()    { vp.stream.WriteString("q\n") }
func (vp *VectorPage) Restore() { vp.stream.WriteString("Q\n") }

func (vp *VectorPage) PushClip(x, y, w, h, radius float64) {
	vp.stream.WriteString("q\n")
	vp.appendRoundRectPath(x, y, w, h, radius)
	vp.stream.WriteString("W n\n")
}

func (vp *VectorPage) PopClip() { vp.stream.WriteString("Q\n") }

func (vp *VectorPage) appendRoundRectPath(x, y, w, h, r float64) {
	if w <= 0 || h <= 0 {
		return
	}
	r = min(r, min(w/2, h/2))
	y0 := vp.py(y + h)
	if r <= 0.1 {
		fmt.Fprintf(&vp.stream, "%.3f %.3f %.3f %.3f re\n", x, y0, w, h)
		return
	}
	// Top-left (x, vp.py(y)), bottom-right (x+w, vp.py(y+h)) in PDF +Y up.
	const k = 0.55228475
	kr := r * k
	xl, xr := x, x+w
	yb, yt := vp.py(y+h), vp.py(y)

	fmt.Fprintf(&vp.stream, "%.3f %.3f m\n", xl+r, yt)
	fmt.Fprintf(&vp.stream, "%.3f %.3f l\n", xr-r, yt)
	fmt.Fprintf(&vp.stream, "%.3f %.3f %.3f %.3f %.3f %.3f c\n", xr-r+kr, yt, xr, yt-r+kr, xr, yt-r)
	fmt.Fprintf(&vp.stream, "%.3f %.3f l\n", xr, yb+r)
	fmt.Fprintf(&vp.stream, "%.3f %.3f %.3f %.3f %.3f %.3f c\n", xr, yb+r-kr, xr-r+kr, yb, xr-r, yb)
	fmt.Fprintf(&vp.stream, "%.3f %.3f l\n", xl+r, yb)
	fmt.Fprintf(&vp.stream, "%.3f %.3f %.3f %.3f %.3f %.3f c\n", xl+r-kr, yb, xl, yb+r-kr, xl, yb+r)
	fmt.Fprintf(&vp.stream, "%.3f %.3f l\n", xl, yt-r)
	fmt.Fprintf(&vp.stream, "%.3f %.3f %.3f %.3f %.3f %.3f c\n", xl, yt-r+kr, xl+r-kr, yt, xl+r, yt)
	vp.stream.WriteString("h\n")
}

// FillRect draws a filled rectangle with optional rounded corners.
func (vp *VectorPage) FillRect(x, y, w, h, radius float64, c [4]uint8) {
	if c[3] == 0 || w <= 0 || h <= 0 {
		return
	}
	vp.setFillRGB(c)
	vp.appendRoundRectPath(x, y, w, h, radius)
	vp.stream.WriteString("f\n")
}

// StrokeRect draws a stroked rectangle outline with optional dash pattern.
func (vp *VectorPage) StrokeRect(x, y, w, h, radius, lineWidth float64, c [4]uint8, dashed bool) {
	if c[3] == 0 || w <= 0 || h <= 0 || lineWidth <= 0 {
		return
	}
	vp.Save()
	vp.setStrokeRGB(c)
	fmt.Fprintf(&vp.stream, "%.3f w 1 J 1 j\n", lineWidth)
	if dashed {
		vp.stream.WriteString("[4 3] 0 d\n")
	}
	vp.appendRoundRectPath(x, y, w, h, radius)
	vp.stream.WriteString("S\n")
	vp.Restore()
}

// Line draws a line segment in vector coordinates.
func (vp *VectorPage) Line(x1, y1, x2, y2, lineWidth float64, c [4]uint8, dashed bool) {
	if c[3] == 0 || lineWidth <= 0 {
		return
	}
	vp.Save()
	vp.setStrokeRGB(c)
	fmt.Fprintf(&vp.stream, "%.3f w 1 J\n", lineWidth)
	if dashed {
		vp.stream.WriteString("[4 3] 0 d\n")
	}
	fmt.Fprintf(&vp.stream, "%.3f %.3f m %.3f %.3f l S\n", x1, vp.py(y1), x2, vp.py(y2))
	vp.Restore()
}

// PathSegment describes an operator in top-down PDF coordinates.
type PathSegment struct {
	Op int // 0 move, 1 line, 2 quad, 3 cube, 4 close
	X0 float64
	Y0 float64
	X1 float64
	Y1 float64
	X2 float64
	Y2 float64
}

// DrawPath fills and/or strokes a custom vector path.
func (vp *VectorPage) DrawPath(segs []PathSegment, fill, stroke [4]uint8, lineWidth float64, dashed bool) {
	if len(segs) == 0 || (fill[3] == 0 && (stroke[3] == 0 || lineWidth <= 0)) {
		return
	}
	doFill := fill[3] > 0
	doStroke := stroke[3] > 0 && lineWidth > 0

	emit := func() {
		var curX, curY float64
		for _, s := range segs {
			switch s.Op {
			case 0:
				curX, curY = s.X0, s.Y0
				fmt.Fprintf(&vp.stream, "%.3f %.3f m\n", curX, vp.py(curY))
			case 1:
				curX, curY = s.X0, s.Y0
				fmt.Fprintf(&vp.stream, "%.3f %.3f l\n", curX, vp.py(curY))
			case 2:
				// Quadratic Bezier (cur, (X0,Y0), (X1,Y1)) -> cubic in top-down coords, then convert Y.
				c1x := curX + (2.0/3.0)*(s.X0-curX)
				c1y := curY + (2.0/3.0)*(s.Y0-curY)
				c2x := s.X1 + (2.0/3.0)*(s.X0-s.X1)
				c2y := s.Y1 + (2.0/3.0)*(s.Y0-s.Y1)
				fmt.Fprintf(&vp.stream, "%.3f %.3f %.3f %.3f %.3f %.3f c\n",
					c1x, vp.py(c1y), c2x, vp.py(c2y), s.X1, vp.py(s.Y1))
				curX, curY = s.X1, s.Y1
			case 3:
				fmt.Fprintf(&vp.stream, "%.3f %.3f %.3f %.3f %.3f %.3f c\n",
					s.X0, vp.py(s.Y0), s.X1, vp.py(s.Y1), s.X2, vp.py(s.Y2))
				curX, curY = s.X2, s.Y2
			case 4:
				vp.stream.WriteString("h\n")
			}
		}
	}

	if doFill {
		vp.Save()
		vp.setFillRGB(fill)
		emit()
		vp.stream.WriteString("f\n")
		vp.Restore()
	}
	if doStroke {
		vp.Save()
		vp.setStrokeRGB(stroke)
		fmt.Fprintf(&vp.stream, "%.3f w 1 J 1 j\n", lineWidth)
		if dashed {
			vp.stream.WriteString("[4 3] 0 d\n")
		}
		emit()
		vp.stream.WriteString("S\n")
		vp.Restore()
	}
}

// DrawCircle draws a filled and/or stroked vector circle.
func (vp *VectorPage) DrawCircle(cx, cy, r float64, fill, stroke [4]uint8, lineWidth float64) {
	if r <= 0 {
		return
	}
	const k = 0.55228475
	kr := r * k
	segs := []PathSegment{
		{Op: 0, X0: cx, Y0: cy - r},
		{Op: 3, X0: cx + kr, Y0: cy - r, X1: cx + r, Y1: cy - kr, X2: cx + r, Y2: cy},
		{Op: 3, X0: cx + r, Y0: cy + kr, X1: cx + kr, Y1: cy + r, X2: cx, Y2: cy + r},
		{Op: 3, X0: cx - kr, Y0: cy + r, X1: cx - r, Y1: cy + kr, X2: cx - r, Y2: cy},
		{Op: 3, X0: cx - r, Y0: cy - kr, X1: cx - kr, Y1: cy - r, X2: cx, Y2: cy - r},
		{Op: 4},
	}
	vp.DrawPath(segs, fill, stroke, lineWidth, false)
}

// DrawText draws vector outlined glyphs (with /ToUnicode support) positioned
// from (x, baselineY). If targetW > 0, mild horizontal Tz scaling keeps long
// or CJK lines cleanly inside their Native-UI measured box without clipping.
func (vp *VectorPage) DrawText(text string, x, baselineY, targetW, size float64, style TextStyle, c [4]uint8) {
	if text == "" || size <= 0 || c[3] == 0 {
		return
	}
	type piece struct {
		isType3 bool
		alias   string
		objID   int
		code    byte
		ascii   byte
		w       float64
	}
	var parts []piece
	totalW := 0.0
	for _, r := range text {
		if r == '\u200b' || r == '\ufeff' || r == '\r' || r == '\n' {
			continue
		}
		if ref, ok := vp.writer.fm.resolveRune(r, style, vp.writer.allocID); ok {
			parts = append(parts, piece{
				isType3: true,
				alias:   ref.subset.alias,
				objID:   ref.subset.objID,
				code:    ref.code,
				w:       ref.width * size,
			})
			totalW += ref.width * size
			continue
		}
		if r >= 0x20 && r <= 0x7e {
			alias, _, objID := vp.writer.fm.base14For(style, vp.writer.allocID)
			w := 0.55 * size
			parts = append(parts, piece{
				alias: alias,
				objID: objID,
				ascii: byte(r),
				w:     w,
			})
			totalW += w
		}
	}
	if len(parts) == 0 {
		return
	}

	tz := 100.0
	if targetW > 0 && totalW > 0.1 {
		ratio := targetW / totalW
		if ratio < 1.0 && ratio >= 0.72 {
			tz = 100.0 * ratio
		} else if ratio > 1.0 && ratio <= 1.08 {
			tz = 100.0 * ratio
		}
	}

	vp.Save()
	vp.setFillRGB(c)
	fmt.Fprintf(&vp.stream, "BT 1 0 0 1 %.3f %.3f Tm %.2f Tz\n", x, vp.py(baselineY), tz)

	// Coalesce consecutive bytes targeting the same font subset/Base-14 dictionary.
	for i := 0; i < len(parts); {
		p0 := parts[i]
		vp.fonts[p0.alias] = p0.objID
		fmt.Fprintf(&vp.stream, "%s %.3f Tf ", p0.alias, size)
		j := i
		if p0.isType3 {
			vp.stream.WriteByte('<')
			for j < len(parts) && parts[j].isType3 && parts[j].alias == p0.alias {
				fmt.Fprintf(&vp.stream, "%02X", parts[j].code)
				j++
			}
			vp.stream.WriteString("> Tj\n")
		} else {
			vp.stream.WriteByte('(')
			for j < len(parts) && !parts[j].isType3 && parts[j].alias == p0.alias {
				b := parts[j].ascii
				if b == '\\' || b == '(' || b == ')' {
					vp.stream.WriteByte('\\')
				}
				vp.stream.WriteByte(b)
				j++
			}
			vp.stream.WriteString(") Tj\n")
		}
		i = j
	}
	vp.stream.WriteString("ET\n")
	vp.Restore()
}

// DrawImage embeds a bitmap image (opaque JPEG or RGBA Flate + /SMask) on the page.
func (vp *VectorPage) DrawImage(img image.Image, x, y, w, h, radius float64) {
	if img == nil || w <= 0 || h <= 0 {
		return
	}
	b := img.Bounds()
	pw, ph := b.Dx(), b.Dy()
	if pw <= 0 || ph <= 0 {
		return
	}

	hasAlpha := false
	rgb := make([]byte, pw*ph*3)
	alpha := make([]byte, pw*ph)
	offRGB, offA := 0, 0
	for yy := b.Min.Y; yy < b.Max.Y; yy++ {
		for xx := b.Min.X; xx < b.Max.X; xx++ {
			r32, g32, b32, a32 := img.At(xx, yy).RGBA()
			a8 := uint8(a32 >> 8)
			if a8 != 255 {
				hasAlpha = true
			}
			// Convert Go's alpha-premultiplied RGBA() back to straight RGB for PDF /SMask compositing.
			if a32 > 0 && a32 < 0xffff {
				rgb[offRGB] = uint8(min(255, (r32*255+a32/2)/a32))
				rgb[offRGB+1] = uint8(min(255, (g32*255+a32/2)/a32))
				rgb[offRGB+2] = uint8(min(255, (b32*255+a32/2)/a32))
			} else {
				rgb[offRGB] = uint8(r32 >> 8)
				rgb[offRGB+1] = uint8(g32 >> 8)
				rgb[offRGB+2] = uint8(b32 >> 8)
			}
			alpha[offA] = a8
			offRGB += 3
			offA++
		}
	}

	imgID := vp.writer.allocID()
	if !hasAlpha {
		var jbuf bytes.Buffer
		if err := jpeg.Encode(&jbuf, img, &jpeg.Options{Quality: 90}); err == nil {
			vp.writer.object(imgID, fmt.Sprintf(
				"<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>",
				pw, ph, jbuf.Len()), jbuf.Bytes())
		} else {
			comp := flateCompress(rgb)
			vp.writer.object(imgID, fmt.Sprintf(
				"<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>",
				pw, ph, len(comp)), comp)
		}
	} else {
		smaskID := vp.writer.allocID()
		compA := flateCompress(alpha)
		vp.writer.object(smaskID, fmt.Sprintf(
			"<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>",
			pw, ph, len(compA)), compA)
		compRGB := flateCompress(rgb)
		vp.writer.object(imgID, fmt.Sprintf(
			"<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask %d 0 R /Filter /FlateDecode /Length %d >>",
			pw, ph, smaskID, len(compRGB)), compRGB)
	}

	vp.nextImg++
	alias := fmt.Sprintf("/Im%d", vp.nextImg)
	vp.images = append(vp.images, vectorImageObj{alias: alias, objID: imgID})

	if radius > 0.1 {
		vp.PushClip(x, y, w, h, radius)
	} else {
		vp.Save()
	}
	fmt.Fprintf(&vp.stream, "%.4f 0 0 %.4f %.4f %.4f cm %s Do\n", w, h, x, vp.py(y+h), alias)
	vp.Restore()
}

// AddLinkURI adds a clickable external hyperlink over rect (x, y, w, h).
func (vp *VectorPage) AddLinkURI(x, y, w, h float64, uri string) {
	if uri == "" || w <= 0 || h <= 0 {
		return
	}
	vp.links = append(vp.links, LinkAnnot{X: x, Y: y, W: w, H: h, URI: uri})
}

// AddLinkDest adds a clickable internal jump to targetPage (0-based) at top-down targetY.
func (vp *VectorPage) AddLinkDest(x, y, w, h float64, targetPage int, targetY float64) {
	if targetPage < 0 || w <= 0 || h <= 0 {
		return
	}
	vp.links = append(vp.links, LinkAnnot{X: x, Y: y, W: w, H: h, TargetPage: targetPage, TargetY: targetY})
}

// FinishVectorPage compresses the page's content stream and writes its /Page
// and /Annot objects to disk immediately.
func (w *Writer) FinishVectorPage(vp *VectorPage) error {
	if w.closed {
		return fmt.Errorf("pdf: writer is closed")
	}
	if w.err != nil {
		return w.err
	}
	if vp == nil {
		return fmt.Errorf("pdf: nil page")
	}

	w.pages = append(w.pages, vp.objID)
	w.lastHeight = vp.Height

	var extGS strings.Builder
	if len(vp.gsByAlpha) > 0 {
		extGS.WriteString("/ExtGState << ")
		alphas := make([]int, 0, len(vp.gsByAlpha))
		for a := range vp.gsByAlpha {
			alphas = append(alphas, int(a))
		}
		sort.Ints(alphas)
		for _, a := range alphas {
			name := vp.gsByAlpha[uint16(a)]
			alphaVal := float64(a) / 255.0
			gsID := w.ensureAlphaGS(uint16(a), alphaVal)
			fmt.Fprintf(&extGS, "%s %d 0 R ", name, gsID)
		}
		extGS.WriteString(">> ")
	}

	var fontDict strings.Builder
	if len(vp.fonts) > 0 {
		fontDict.WriteString("/Font << ")
		aliases := make([]string, 0, len(vp.fonts))
		for alias := range vp.fonts {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		for _, alias := range aliases {
			fmt.Fprintf(&fontDict, "%s %d 0 R ", alias, vp.fonts[alias])
		}
		fontDict.WriteString(">> ")
	}

	var xobjDict strings.Builder
	if len(vp.images) > 0 {
		xobjDict.WriteString("/XObject << ")
		for _, im := range vp.images {
			fmt.Fprintf(&xobjDict, "%s %d 0 R ", im.alias, im.objID)
		}
		xobjDict.WriteString(">> ")
	}

	var annotDict strings.Builder
	if len(vp.links) > 0 {
		annotDict.WriteString("/Annots [ ")
		for _, lnk := range vp.links {
			annID := w.allocID()
			llx, lly := lnk.X, vp.Height-(lnk.Y+lnk.H)
			urx, ury := lnk.X+lnk.W, vp.Height-lnk.Y
			if lnk.URI != "" {
				w.object(annID, fmt.Sprintf(
					"<< /Type /Annot /Subtype /Link /Rect [%.2f %.2f %.2f %.2f] /Border [0 0 0] /A << /S /URI /URI %s >> >>",
					llx, lly, urx, ury, literalString(lnk.URI)), nil)
			} else {
				targetID := w.pageObjID(lnk.TargetPage)
				pdfTargetY := max(0, vp.Height-lnk.TargetY)
				w.object(annID, fmt.Sprintf(
					"<< /Type /Annot /Subtype /Link /Rect [%.2f %.2f %.2f %.2f] /Border [0 0 0] /Dest [%d 0 R /XYZ 0 %.2f null] >>",
					llx, lly, urx, ury, targetID, pdfTargetY), nil)
			}
			fmt.Fprintf(&annotDict, "%d 0 R ", annID)
		}
		annotDict.WriteString("] ")
	}

	comp := flateCompress(vp.stream.Bytes())
	contentID := w.allocID()
	w.object(contentID, fmt.Sprintf("<< /Filter /FlateDecode /Length %d >>", len(comp)), comp)

	w.object(vp.objID, fmt.Sprintf(
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << %s%s%s>> %s/Contents %d 0 R >>",
		vp.Width, vp.Height, fontDict.String(), extGS.String(), xobjDict.String(), annotDict.String(), contentID,
	), nil)
	return w.err
}

func literalString(s string) string {
	var b strings.Builder
	b.WriteByte('(')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '(' || c == ')' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte(')')
	return b.String()
}

func (w *Writer) writeOutlines(rootID int) {
	items := w.outlines
	if len(items) == 0 {
		return
	}
	type oNode struct {
		objID    int
		item     OutlineItem
		parent   int
		children []int
	}
	nodes := make([]oNode, len(items))
	for i, it := range items {
		nodes[i] = oNode{objID: w.allocID(), item: it, parent: -1}
	}
	var top []int
	var stack []int
	for i := range nodes {
		lvl := max(1, nodes[i].item.Level)
		for len(stack) > 0 && max(1, nodes[stack[len(stack)-1]].item.Level) >= lvl {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			nodes[i].parent = -1
			top = append(top, i)
		} else {
			p := stack[len(stack)-1]
			nodes[i].parent = p
			nodes[p].children = append(nodes[p].children, i)
		}
		stack = append(stack, i)
	}

	// Sibling chains for parent -> siblings slice.
	prevOf := make([]int, len(nodes))
	nextOf := make([]int, len(nodes))
	for i := range nodes {
		prevOf[i], nextOf[i] = -1, -1
	}
	linkSiblings := func(list []int) {
		for k, idx := range list {
			if k > 0 {
				prevOf[idx] = list[k-1]
			}
			if k+1 < len(list) {
				nextOf[idx] = list[k+1]
			}
		}
	}
	linkSiblings(top)
	for i := range nodes {
		linkSiblings(nodes[i].children)
	}

	defaultH := A4Height
	if w.lastHeight > 0 {
		defaultH = w.lastHeight
	}

	w.object(rootID, fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Last %d 0 R /Count %d >>",
		nodes[top[0]].objID, nodes[top[len(top)-1]].objID, len(top)), nil)

	for i, n := range nodes {
		var sb strings.Builder
		parentID := rootID
		if n.parent >= 0 {
			parentID = nodes[n.parent].objID
		}
		pageIdx := min(max(0, n.item.Page), max(0, len(w.pages)-1))
		pageID := w.pageObjID(pageIdx)
		pdfY := max(0, defaultH-n.item.Y)
		fmt.Fprintf(&sb, "<< /Title %s /Parent %d 0 R /Dest [%d 0 R /XYZ 0 %.2f null]",
			textString(n.item.Title), parentID, pageID, pdfY)
		if prevOf[i] >= 0 {
			fmt.Fprintf(&sb, " /Prev %d 0 R", nodes[prevOf[i]].objID)
		}
		if nextOf[i] >= 0 {
			fmt.Fprintf(&sb, " /Next %d 0 R", nodes[nextOf[i]].objID)
		}
		if len(n.children) > 0 {
			fmt.Fprintf(&sb, " /First %d 0 R /Last %d 0 R /Count %d",
				nodes[n.children[0]].objID, nodes[n.children[len(n.children)-1]].objID, len(n.children))
		}
		sb.WriteString(" >>")
		w.object(n.objID, sb.String(), nil)
	}
}
