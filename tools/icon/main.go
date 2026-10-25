// Command icon draws Tern's app icon: the bird as a character — a big cream
// head under the black cap, one round eye, the long orange bill.
//
//	go run ./tools/icon            # all formats, into assets/brand
//	go run ./tools/icon -try out.png  # one 1024 draft
//
// One description drives every size and every format, so the Dock icon, the
// .ico and the SVG cannot drift apart.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// ------------------------------------------------------------------ colour

type rgb struct{ r, g, b float64 }

func hex(s string) rgb {
	s = strings.TrimPrefix(s, "#")
	n := func(i int) float64 {
		var v float64
		for _, c := range s[i : i+2] {
			v *= 16
			switch {
			case c >= '0' && c <= '9':
				v += float64(c - '0')
			case c >= 'a' && c <= 'f':
				v += float64(c-'a') + 10
			case c >= 'A' && c <= 'F':
				v += float64(c-'A') + 10
			}
		}
		return v
	}
	return rgb{n(0), n(2), n(4)}
}

func (c rgb) css() string {
	return fmt.Sprintf("#%02X%02X%02X", uint8(math.Round(c.r)), uint8(math.Round(c.g)), uint8(math.Round(c.b)))
}

func (c rgb) nrgba(a uint8) color.NRGBA {
	return color.NRGBA{uint8(math.Round(c.r)), uint8(math.Round(c.g)), uint8(math.Round(c.b)), a}
}

func mix(a, b rgb, t float64) rgb {
	return rgb{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}

// ------------------------------------------------------------------ paths

type pt struct{ x, y float64 }

// path is a list of SVG-style commands. Curves stay curves all the way to the
// SVG file and are flattened only by the rasteriser.
type path struct{ c []cmd }

type cmd struct {
	op byte // 'M' move, 'L' line, 'C' cubic, 'Z' close
	p  []pt
}

func (p *path) move(x, y float64) { p.c = append(p.c, cmd{'M', []pt{{x, y}}}) }

func (p *path) line(x, y float64) { p.c = append(p.c, cmd{'L', []pt{{x, y}}}) }

func (p *path) cube(x1, y1, x2, y2, x, y float64) {
	p.c = append(p.c, cmd{'C', []pt{{x1, y1}, {x2, y2}, {x, y}}})
}

func (p *path) close() { p.c = append(p.c, cmd{'Z', nil}) }

// xf returns a copy with f applied to every point; used to place the mark on
// the icon grid.
func (p path) xf(f func(pt) pt) path {
	out := path{c: make([]cmd, len(p.c))}
	for i, c := range p.c {
		q := cmd{op: c.op}
		for _, v := range c.p {
			q.p = append(q.p, f(v))
		}
		out.c[i] = q
	}
	return out
}

func (p path) box() (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, c := range p.c {
		for _, q := range c.p {
			minX, minY = math.Min(minX, q.x), math.Min(minY, q.y)
			maxX, maxY = math.Max(maxX, q.x), math.Max(maxY, q.y)
		}
	}
	return
}

// ------------------------------------------------------------------ shapes

type shape struct {
	p   path
	col rgb
}

type doc struct {
	tile   [2]rgb
	shapes []shape
}

// tilePath is the icon's rounded square: Apple's 824 grid, radius 185.
func tilePath() path {
	const x, y, s, r = 100.0, 100.0, 824.0, 185.0
	const k = 0.5522847498
	var p path
	p.move(x+r, y)
	p.line(x+s-r, y)
	p.cube(x+s-r+r*k, y, x+s, y+r-r*k, x+s, y+r)
	p.line(x+s, y+s-r)
	p.cube(x+s, y+s-r+r*k, x+s-r+r*k, y+s, x+s-r, y+s)
	p.line(x+r, y+s)
	p.cube(x+r-r*k, y+s, x, y+s-r+r*k, x, y+s-r)
	p.line(x, y+r)
	p.cube(x, y+r-r*k, x+r-r*k, y, x+r, y)
	p.close()
	return p
}

// disc is a circle in four cubic arcs.
func disc(cx, cy, r float64) path {
	const k = 0.5522847498
	var p path
	p.move(cx+r, cy)
	p.cube(cx+r, cy+r*k, cx+r*k, cy+r, cx, cy+r)
	p.cube(cx-r*k, cy+r, cx-r, cy+r*k, cx-r, cy)
	p.cube(cx-r, cy-r*k, cx-r*k, cy-r, cx, cy-r)
	p.cube(cx+r*k, cy-r, cx+r, cy-r*k, cx+r, cy)
	p.close()
	return p
}

// ------------------------------------------------------------------ the tern

type palette struct {
	name  string
	tile  [2]rgb
	bird  rgb
	cap   rgb
	beak  rgb
	eye   rgb
	blush rgb
}

var (
	// prod: polar night behind a cream tern.
	prod = palette{
		name:  "prod",
		tile:  [2]rgb{hex("#17505C"), hex("#226B79")},
		bird:  hex("#F8F0DD"),
		cap:   hex("#08222A"),
		beak:  hex("#E8873C"),
		eye:   hex("#0D2A33"),
		blush: hex("#EFC3A2"),
	}
	// dev: polar day, the same character in negative. Same shapes, so a
	// development build is obvious in the Dock without a badge.
	dev = palette{
		name:  "dev",
		tile:  [2]rgb{hex("#C2653C"), hex("#AE4E2C")},
		bird:  hex("#FBF3E2"),
		cap:   hex("#2B1610"),
		beak:  hex("#E07A38"),
		eye:   hex("#2B1610"),
		blush: hex("#E79A76"),
	}
)

// arc appends the circular arc from a0 to a1 (radians, a0 < a1) as cubics, so
// the cap can follow the head's own outline exactly.
func (p *path) arc(cx, cy, r, a0, a1 float64) {
	for a1-a0 > 1e-6 {
		step := math.Min(math.Pi/2, a1-a0)
		b := a0 + step
		k := 4.0 / 3.0 * math.Tan(step/4)
		x0, y0 := cx+r*math.Cos(a0), cy+r*math.Sin(a0)
		x1, y1 := cx+r*math.Cos(b), cy+r*math.Sin(b)
		p.cube(
			x0-k*r*math.Sin(a0), y0+k*r*math.Cos(a0),
			x1+k*r*math.Sin(b), y1-k*r*math.Cos(b),
			x1, y1)
		a0 = b
	}
}

// ellipse is a circle with independent radii, for the eye.
func ellipse(cx, cy, rx, ry float64) path {
	const k = 0.5522847498
	var p path
	p.move(cx+rx, cy)
	p.cube(cx+rx, cy+ry*k, cx+rx*k, cy+ry, cx, cy+ry)
	p.cube(cx-rx*k, cy+ry, cx-rx, cy+ry*k, cx-rx, cy)
	p.cube(cx-rx, cy-ry*k, cx-rx*k, cy-ry, cx, cy-ry)
	p.cube(cx+rx*k, cy-ry, cx+rx, cy-ry*k, cx+rx, cy)
	p.close()
	return p
}

// tern draws the character: a big round head under the black cap, one eye, the
// long orange bill. Coordinates are in the 1024 icon grid.
func tern(pl palette) *doc {
	const cx, cy, r = 450.0, 640.0, 320.0

	head := disc(cx, cy, r)

	// cap: the head's upper arc, closed by a wave that sweeps down to the nape.
	const aL, aR = 175.0 * math.Pi / 180, 340.0 * math.Pi / 180
	cap := path{}
	cap.move(cx+r*math.Cos(aL), cy+r*math.Sin(aL))
	cap.arc(cx, cy, r, aL, aR)
	cap.cube(690, 600, 590, 640, 480, 662)
	cap.cube(370, 684, 250, 696, cx+r*math.Cos(aL), cy+r*math.Sin(aL))
	cap.close()

	bill := path{}
	bill.move(736, 636)
	bill.cube(812, 652, 872, 678, 908, 710)
	bill.cube(858, 730, 786, 742, 742, 748)
	bill.close()

	eye := ellipse(600, 700, 42, 50)
	glint := disc(582, 674, 12)

	d := &doc{tile: pl.tile}
	blush := ellipse(628, 800, 46, 25)

	d.shapes = []shape{
		{head, pl.bird},
		{cap, pl.cap},
		{bill, pl.beak},
		{blush, pl.blush},
		{eye, pl.eye},
		{glint, pl.bird},
	}
	return d
}

func (d *doc) box() (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, s := range d.shapes {
		a, b, c, e := s.p.box()
		minX, minY = math.Min(minX, a), math.Min(minY, b)
		maxX, maxY = math.Max(maxX, c), math.Max(maxY, e)
	}
	return
}

// ------------------------------------------------------------------ raster

type canvas struct {
	img  *image.RGBA
	size int
}

func newCanvas(size int) *canvas {
	return &canvas{img: image.NewRGBA(image.Rect(0, 0, size, size)), size: size}
}

// mask rasterises a path with the anti-aliased vector rasteriser.
func (c *canvas) mask(p path) *image.Alpha {
	rz := vector.NewRasterizer(c.size, c.size)
	rz.DrawOp = draw.Src
	k := float64(c.size) / 1024
	for _, cmd := range p.c {
		switch cmd.op {
		case 'M':
			rz.MoveTo(float32(cmd.p[0].x*k), float32(cmd.p[0].y*k))
		case 'L':
			rz.LineTo(float32(cmd.p[0].x*k), float32(cmd.p[0].y*k))
		case 'C':
			rz.CubeTo(
				float32(cmd.p[0].x*k), float32(cmd.p[0].y*k),
				float32(cmd.p[1].x*k), float32(cmd.p[1].y*k),
				float32(cmd.p[2].x*k), float32(cmd.p[2].y*k))
		case 'Z':
			rz.ClosePath()
		}
	}
	m := image.NewAlpha(c.img.Bounds())
	rz.Draw(m, m.Bounds(), image.Opaque, image.Point{})
	return m
}

func (c *canvas) fill(p path, src image.Image) {
	draw.DrawMask(c.img, c.img.Bounds(), src, image.Point{}, c.mask(p), image.Point{}, draw.Over)
}

func (c *canvas) solid(p path, col color.NRGBA) {
	c.fill(p, image.NewUniform(col))
}

// clip scales the canvas alpha by the mask, so artwork stops at the tile edge.
func (c *canvas) clip(m *image.Alpha) {
	for y := 0; y < c.size; y++ {
		for x := 0; x < c.size; x++ {
			a := uint32(m.AlphaAt(x, y).A)
			if a == 255 {
				continue
			}
			i := c.img.PixOffset(x, y)
			for k := 0; k < 4; k++ {
				c.img.Pix[i+k] = uint8(uint32(c.img.Pix[i+k]) * a / 255)
			}
		}
	}
}

// gradient fills top-to-bottom, easing into the darker end so the light sits
// where the bird is.
func (c *canvas) gradient(top, bottom rgb) image.Image {
	full := image.NewRGBA(c.img.Bounds())
	for y := 0; y < c.size; y++ {
		t := math.Sqrt(float64(y) / float64(c.size-1))
		col := mix(top, bottom, t).nrgba(255)
		draw.Draw(full, image.Rect(0, y, c.size, y+1), image.NewUniform(col), image.Point{}, draw.Src)
	}
	return full
}

func render(d *doc, size int) *image.RGBA {
	c := newCanvas(size)
	tile := c.mask(tilePath())
	c.fill(tilePath(), c.gradient(d.tile[0], d.tile[1]))

	m := newCanvas(size)
	for _, s := range d.shapes {
		m.solid(s.p, s.col.nrgba(255))
	}
	m.clip(tile)
	draw.Draw(c.img, c.img.Bounds(), m.img, image.Point{}, draw.Over)
	return c.img
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// ------------------------------------------------------------------ svg

func svgPath(p path) string {
	var b strings.Builder
	for _, c := range p.c {
		switch c.op {
		case 'M':
			fmt.Fprintf(&b, "M%.1f %.1f", c.p[0].x, c.p[0].y)
		case 'L':
			fmt.Fprintf(&b, "L%.1f %.1f", c.p[0].x, c.p[0].y)
		case 'C':
			fmt.Fprintf(&b, "C%.1f %.1f %.1f %.1f %.1f %.1f",
				c.p[0].x, c.p[0].y, c.p[1].x, c.p[1].y, c.p[2].x, c.p[2].y)
		case 'Z':
			b.WriteString("Z")
		}
	}
	return b.String()
}

func renderSVG(d *doc) string {
	tile := svgPath(tilePath())
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" width="1024" height="1024">` + "\n")
	fmt.Fprintf(&b, `<defs><linearGradient id="tile" x1="0" y1="0" x2="0" y2="1">`+
		`<stop offset="0" stop-color="%s"/>`+
		`<stop offset=".5" stop-color="%s"/>`+
		`<stop offset="1" stop-color="%s"/></linearGradient>`+
		`<clipPath id="tile-clip"><path d="%s"/></clipPath></defs>`+"\n",
		d.tile[0].css(), mix(d.tile[0], d.tile[1], 0.708).css(), d.tile[1].css(), tile)
	fmt.Fprintf(&b, `<path d="%s" fill="url(#tile)"/>`+"\n", tile)
	fmt.Fprintf(&b, `<g clip-path="url(#tile-clip)">`+"\n")
	for _, s := range d.shapes {
		fmt.Fprintf(&b, `<path d="%s" fill="%s"/>`+"\n", svgPath(s.p), s.col.css())
	}
	b.WriteString("</g>\n</svg>\n")
	return b.String()
}

// ------------------------------------------------------------------ icns / ico

func encodePNG(img image.Image) []byte {
	var buf strings.Builder
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&stringWriter{&buf}, img); err != nil {
		log.Fatal(err)
	}
	return []byte(buf.String())
}

type stringWriter struct{ b *strings.Builder }

func (w *stringWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

func writeICNS(path string, imgs map[string]*image.RGBA) error {
	order := []struct {
		typ string
		key string
	}{
		{"ic07", "128"}, {"ic08", "256"}, {"ic09", "512"}, {"ic10", "1024"},
		{"ic11", "32"}, {"ic12", "64"}, {"ic13", "256"}, {"ic14", "512"},
	}
	var body []byte
	for _, e := range order {
		img, ok := imgs[e.key]
		if !ok {
			continue
		}
		data := encodePNG(img)
		hdr := make([]byte, 8)
		copy(hdr, e.typ)
		be32(hdr[4:], uint32(len(data)+8))
		body = append(body, hdr...)
		body = append(body, data...)
	}
	out := make([]byte, 8)
	copy(out, "icns")
	be32(out[4:], uint32(len(body)+8))
	out = append(out, body...)
	return writeFile(path, out)
}

func writeICO(path string, imgs []*image.RGBA) error {
	var data, dir []byte
	hdr := make([]byte, 6)
	hdr[2] = 1
	le16(hdr[4:], uint16(len(imgs)))
	offset := 6 + 16*len(imgs)
	for _, img := range imgs {
		b := encodePNG(img)
		e := make([]byte, 16)
		if s := img.Bounds().Dx(); s < 256 {
			e[0], e[1] = uint8(s), uint8(s)
		}
		le16(e[4:], 1)
		le16(e[6:], 32)
		le32(e[8:], uint32(len(b)))
		le32(e[12:], uint32(offset))
		dir = append(dir, e...)
		data = append(data, b...)
		offset += len(b)
	}
	out := append(hdr, dir...)
	out = append(out, data...)
	return writeFile(path, out)
}

func be32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
}

func le16(b []byte, v uint16) { b[0], b[1] = byte(v), byte(v>>8) }

func le32(b []byte, v uint32) { b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24) }

func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ------------------------------------------------------------------ main

func main() {
	out := flag.String("out", "assets/brand", "output directory")
	try := flag.String("try", "", "write one draft PNG and exit")
	flag.Parse()

	if *try != "" {
		img := render(tern(prod), 1024)
		if err := writePNG(*try, img); err != nil {
			log.Fatalf("icon: %v", err)
		}
		return
	}

	variants := []struct {
		prefix string
		pl     palette
	}{
		{"", prod},
		{"-dev", dev},
	}
	all := map[string]map[string]*image.RGBA{}
	for _, v := range variants {
		d := tern(v.pl)
		if err := writeFile(filepath.Join(*out, "icon"+v.prefix+".svg"), []byte(renderSVG(d))); err != nil {
			log.Fatalf("icon: %v", err)
		}
		sizes := map[string]*image.RGBA{}
		for _, s := range []int{16, 32, 48, 64, 128, 256, 512, 1024} {
			img := render(d, s)
			sizes[fmt.Sprint(s)] = img
			if err := writePNG(filepath.Join(*out, "png", fmt.Sprintf("icon%s-%d.png", v.prefix, s)), img); err != nil {
				log.Fatalf("icon: %v", err)
			}
		}
		all[v.prefix] = sizes
	}

	if err := writeICNS(filepath.Join(*out, "macos/AppIcon.icns"), all[""]); err != nil {
		log.Fatalf("icns: %v", err)
	}
	if err := writeICNS(filepath.Join(*out, "macos/AppIcon-Debug.icns"), all["-dev"]); err != nil {
		log.Fatalf("icns: %v", err)
	}
	if err := writeICO(filepath.Join(*out, "web/favicon.ico"), []*image.RGBA{all[""]["16"], all[""]["32"], all[""]["48"]}); err != nil {
		log.Fatalf("ico: %v", err)
	}
	for _, s := range []struct {
		name string
		size int
	}{{"apple-touch-icon.png", 180}, {"icon-192.png", 192}, {"icon-512.png", 512}} {
		if err := writePNG(filepath.Join(*out, "web", s.name), render(tern(prod), s.size)); err != nil {
			log.Fatalf("icon: %v", err)
		}
	}
	if err := writeSheet(filepath.Join(*out, "preview.png"), tern(prod), tern(dev)); err != nil {
		log.Fatalf("preview: %v", err)
	}
	fmt.Println("wrote", *out)
}

// label writes one line of text on the sheet.
func label(img *image.RGBA, x, y int, text string) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.RGBA{90, 100, 104, 255}),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

// writeSheet lays the icons out the way they are judged by eye: both variants
// large, the size ramp, and a strip on a dark Dock.
func writeSheet(path string, prodDoc, devDoc *doc) error {
	const w, h = 1600, 1390
	sh := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(sh, sh.Bounds(), image.NewUniform(color.RGBA{247, 245, 240, 255}), image.Point{}, draw.Src)
	put := func(img *image.RGBA, x, y int) {
		draw.Draw(sh, image.Rect(x, y, x+img.Bounds().Dx(), y+img.Bounds().Dy()), img, image.Point{}, draw.Over)
	}
	put(render(prodDoc, 440), 80, 60)
	put(render(devDoc, 440), 600, 60)
	label(sh, 80, 540, "prod - icon.svg")
	label(sh, 600, 540, "dev - icon-dev.svg")

	x := 80
	for _, s := range []int{256, 128, 64, 32, 16} {
		put(render(prodDoc, s), x, 620)
		x += s + 34
	}
	x = 80
	for _, s := range []int{256, 128, 64, 32, 16} {
		put(render(devDoc, s), x, 930)
		x += s + 34
	}

	dock := image.NewRGBA(image.Rect(0, 0, 1120, 130))
	draw.Draw(dock, dock.Bounds(), image.NewUniform(color.RGBA{26, 26, 28, 255}), image.Point{}, draw.Src)
	dx := 26
	for _, s := range []int{96, 64, 48, 32, 24, 16} {
		a, b := render(prodDoc, s), render(devDoc, s)
		putDock := func(img *image.RGBA, x int) {
			draw.Draw(dock, image.Rect(x, 65-s/2, x+s, 65-s/2+s), img, image.Point{}, draw.Over)
		}
		putDock(a, dx)
		putDock(b, dx+s+18)
		dx += 2*s + 58
	}
	draw.Draw(sh, image.Rect(80, 1230, 80+dock.Bounds().Dx(), 1230+dock.Bounds().Dy()), dock, image.Point{}, draw.Over)
	return writePNG(path, sh)
}
