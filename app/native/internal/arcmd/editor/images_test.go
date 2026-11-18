package editor

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func imageTester(markdown string, width int, resource ImageResource, choose func(string, string), reveal func(string)) (*Editor, *ui.Tester) {
	e := New(markdown)
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, e, Options{Width: float32(width), Theme: c.Theme(),
			LoadImage: func(string) ImageResource { return resource }, ReplaceImage: choose, RevealImage: reveal})
	}, width, 900)
	tt.Frame()
	return e, tt
}

func TestImageNaturalSizeAndCenter(t *testing.T) {
	for _, width := range []int{320, 900} {
		for _, size := range []image.Point{{80, 60}, {1200, 600}, {480, 1600}} {
			t.Run(fmt.Sprintf("%d-%v", width, size), func(t *testing.T) {
				bitmap := ui.NewBitmap(image.NewRGBA(image.Rectangle{Max: size}))
				e, _ := imageTester("![说明](image.png)\n\nFollowing paragraph", width, ImageResource{Bitmap: bitmap}, nil, nil)
				r := e.rows[0]
				p := r.picture.bounds
				if math.Abs(float64(p.X+p.W/2-r.rect.X-r.rect.W/2)) > .01 {
					t.Fatal("picture is not centered", p, r.rect)
				}
				if p.W > r.rect.W || p.W > float32(size.X) || math.Abs(float64(p.W/p.H-float32(size.X)/float32(size.Y))) > .01 {
					t.Fatal("image is stretched, cropped, or enlarged", p)
				}
				if size.Y == 1600 && p.H <= 360 {
					t.Fatal("tall picture is still height-capped", p)
				}
				if e.rows[1].rect.Y < p.Y+p.H+18 {
					t.Fatal("picture overlaps following text")
				}
			})
		}
	}
}

func TestMissingImageCardWrapsAndActionsWork(t *testing.T) {
	for _, width := range []int{320, 900} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			src := "../images/" + strings.Repeat("long-file-name-", 5) + ".png"
			var chosenID, chosenURL, revealed string
			e, tt := imageTester("!["+strings.Repeat("图片说明", 20)+"]("+src+")\n\nFollowing paragraph", width,
				ImageResource{Problem: ImageUnsupported, Local: true},
				func(id, url string) { chosenID, chosenURL = id, url }, func(url string) { revealed = url })
			f := e.rows[0].picture
			if f == nil || len(f.actions) != 2 {
				t.Fatal("missing card actions")
			}
			for _, action := range f.actions {
				r := action.bounds
				if r.X < f.bounds.X || r.X+r.W > f.bounds.X+f.bounds.W || r.Y+r.H > f.bounds.Y+f.bounds.H {
					t.Fatal("action overflow", r, f.bounds)
				}
			}
			if width == 320 && f.actions[0].bounds.Y == f.actions[1].bounds.Y {
				t.Fatal("narrow actions did not wrap")
			}
			if e.rows[1].rect.Y < f.bounds.Y+f.bounds.H+18 {
				t.Fatal("card overlaps following text")
			}
			if err := tt.Click("重新选择图片…"); err != nil {
				t.Fatal(err)
			}
			if chosenID != e.Doc.Blocks[0].ID || chosenURL != src {
				t.Fatal("choose targeted another image")
			}
			tt.Key(0, ui.KeyTab)
			if !tt.Focused("在文件夹中显示") {
				t.Fatal("image action is not keyboard reachable")
			}
			tt.Key(0, ui.KeyEnter)
			if revealed != src {
				t.Fatal("reveal did not run")
			}
		})
	}
}

func TestImagePreviewKeyboardAndEscape(t *testing.T) {
	bitmap := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 1200, 1600)))
	e, tt := imageTester("![description](photo.png)", 900, ImageResource{Bitmap: bitmap}, nil, nil)
	if err := tt.Click("查看图片：photo.png"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	if !e.imageViewer.open || !tt.HasText("原始大小") {
		t.Fatal("preview did not open")
	}
	if err := tt.Click("原始大小"); err != nil || !e.imageViewer.actual {
		t.Fatal("actual size", err)
	}
	tt.Key(0, ui.KeyEscape)
	if e.imageViewer.open {
		t.Fatal("Escape did not close preview")
	}
}

func TestImageGeometryFollowsIncrementalEdits(t *testing.T) {
	e, tt := imageTester("Before\n\n![description](photo.png)\n\nAfter", 600, ImageResource{Problem: ImageMissing, Local: true}, func(string, string) {}, func(string) {})
	before := e.rows[1].picture.bounds.Y
	focus(e, tt)
	e.SetSelection(0, 0)
	tt.Type(strings.Repeat("words ", 40))
	shifted := e.rows[1].picture.bounds
	button := e.rows[1].picture.actions[0].bounds
	if shifted.Y <= before {
		t.Fatal("picture did not move with the paragraph")
	}
	e.ready = false
	tt.Frame()
	if e.rows[1].picture.bounds != shifted || e.rows[1].picture.actions[0].bounds != button {
		t.Fatal("incremental image geometry differs from a full layout")
	}
}

func TestImagePreviewDoesNotStealTyping(t *testing.T) {
	bitmap := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 80, 60)))
	e, tt := imageTester("Before\n\n![description](photo.png)", 600, ImageResource{Bitmap: bitmap}, nil, nil)
	focus(e, tt)
	e.SetSelection(6, 6)
	tt.Key(0, ui.KeyEnter)
	tt.Type("After")
	if e.imageViewer.open || len(e.Doc.Blocks) != 3 || e.Doc.Blocks[1].Text() != "After" {
		t.Fatal("picture controls intercepted typing")
	}
}

func TestImageSelectionDeleteAndUndo(t *testing.T) {
	bitmap := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 80, 60)))
	e, tt := imageTester("Before\n\n![description](photo.png)\n\nAfter", 600, ImageResource{Bitmap: bitmap}, nil, nil)
	if err := tt.Click("查看图片：photo.png"); err != nil {
		t.Fatal(err)
	}
	if e.Engine.LeafAt(e.Caret).Block.Kind != "image" {
		t.Fatal("picture click left the caret on another block")
	}
	tt.Key(0, ui.KeyDelete)
	if len(e.Doc.Blocks) != 2 {
		t.Fatal("selected picture did not delete")
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	if len(e.Doc.Blocks) != 3 || e.Doc.Blocks[1].URL != "photo.png" {
		t.Fatal("picture delete could not be undone")
	}
}

func TestAnimatedFrameAt(t *testing.T) {
	a, b := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 1, 1))), ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 1, 1)))
	r := ImageResource{Bitmap: a, Frames: []*ui.Bitmap{a, b}, Delays: []time.Duration{100 * time.Millisecond, 300 * time.Millisecond}}
	base := time.Unix(0, 0)
	if f, next := r.frameAt(base.Add(50 * time.Millisecond)); f != a || next != 50*time.Millisecond {
		t.Fatal("first frame", next)
	}
	if f, next := r.frameAt(base.Add(150 * time.Millisecond)); f != b || next != 250*time.Millisecond {
		t.Fatal("second frame", next)
	}
	if f, next := (ImageResource{Bitmap: a}).frameAt(base); f != a || next != 0 {
		t.Fatal("still")
	}
}
