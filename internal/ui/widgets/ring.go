package widgets

import (
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// Ring is a circular gauge: a faint full ring plus a coloured arc whose
// sweep angle is proportional to a 0..1 value. A centred label sits on
// top. It draws into a canvas.Raster image so it scales smoothly.
type Ring struct {
	widget.BaseWidget
	value     float64 // 0..1
	color     color.Color
	track     color.Color
	thickness float64 // ratio of radius (0..1), default 0.18
}

func NewRing(value float64, fg, track color.Color) *Ring {
	r := &Ring{value: clamp01(value), color: fg, track: track, thickness: 0.18}
	r.ExtendBaseWidget(r)
	return r
}

func (r *Ring) Set(value float64, fg color.Color) {
	r.value = clamp01(value)
	r.color = fg
	r.Refresh()
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}

func (r *Ring) MinSize() fyne.Size { return fyne.NewSize(80, 80) }

func (r *Ring) CreateRenderer() fyne.WidgetRenderer {
	raster := canvas.NewRaster(r.render)
	return &ringRenderer{ring: r, raster: raster}
}

func (r *Ring) render(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w == 0 || h == 0 {
		return img
	}
	cx := float64(w) / 2
	cy := float64(h) / 2
	radius := math.Min(cx, cy) - 1
	thick := radius * r.thickness
	if thick < 2 {
		thick = 2
	}
	innerR := radius - thick
	if innerR < 1 {
		innerR = 1
	}

	sweep := r.value * 2 * math.Pi
	// Start at -π/2 (12 o'clock), increase clockwise: angles use atan2 with
	// y inverted so "12 o'clock" is angle 0 after a transform.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := float64(x) - cx + 0.5
			dy := float64(y) - cy + 0.5
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > radius+1 || dist < innerR-1 {
				continue
			}
			// Smooth edges of the ring band.
			alpha := 1.0
			if dist > radius {
				alpha = radius + 1 - dist
			} else if dist < innerR {
				alpha = dist - (innerR - 1)
			}
			if alpha <= 0 {
				continue
			}

			ang := math.Atan2(dx, -dy) // 0 at 12 o'clock, clockwise positive
			if ang < 0 {
				ang += 2 * math.Pi
			}
			var c color.Color = r.track
			if ang <= sweep {
				c = r.color
			}
			img.Set(x, y, blend(c, alpha))
		}
	}
	return img
}

func blend(c color.Color, alpha float64) color.Color {
	r, g, b, a := c.RGBA()
	out := color.RGBA{
		R: uint8(r >> 8),
		G: uint8(g >> 8),
		B: uint8(b >> 8),
		A: uint8(float64(a>>8) * alpha),
	}
	return out
}

type ringRenderer struct {
	ring   *Ring
	raster *canvas.Raster
}

func (r *ringRenderer) Layout(size fyne.Size) {
	r.raster.Resize(size)
	r.raster.Move(fyne.NewPos(0, 0))
}
func (r *ringRenderer) MinSize() fyne.Size           { return fyne.NewSize(80, 80) }
func (r *ringRenderer) Refresh()                     { r.raster.Refresh() }
func (r *ringRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.raster} }
func (r *ringRenderer) Destroy()                     {}
