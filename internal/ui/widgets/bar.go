package widgets

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// Bar is a thin horizontal progress bar with rounded corners. The fill
// width is value*size.Width. The bar itself is a fixed thin strip; place
// labels above/below using normal containers.
type Bar struct {
	widget.BaseWidget
	value     float64 // 0..1
	color     color.Color
	track     color.Color
	thickness float32
}

func NewBar(value float64, fg, track color.Color, thickness float32) *Bar {
	if thickness <= 0 {
		thickness = 6
	}
	b := &Bar{value: clamp01(value), color: fg, track: track, thickness: thickness}
	b.ExtendBaseWidget(b)
	return b
}

func (b *Bar) Set(value float64, fg color.Color) {
	b.value = clamp01(value)
	b.color = fg
	b.Refresh()
}

func (b *Bar) MinSize() fyne.Size { return fyne.NewSize(40, b.thickness) }

func (b *Bar) CreateRenderer() fyne.WidgetRenderer {
	track := canvas.NewRectangle(b.track)
	track.CornerRadius = b.thickness / 2
	fg := canvas.NewRectangle(b.color)
	fg.CornerRadius = b.thickness / 2
	return &barRenderer{bar: b, track: track, fg: fg}
}

type barRenderer struct {
	bar         *Bar
	track, fg   *canvas.Rectangle
}

func (r *barRenderer) Layout(size fyne.Size) {
	h := r.bar.thickness
	if h > size.Height {
		h = size.Height
	}
	y := (size.Height - h) / 2
	r.track.Resize(fyne.NewSize(size.Width, h))
	r.track.Move(fyne.NewPos(0, y))
	w := size.Width * float32(r.bar.value)
	if w < h {
		// keep a rounded pill even at near-zero values
		w = 0
	}
	r.fg.Resize(fyne.NewSize(w, h))
	r.fg.Move(fyne.NewPos(0, y))
}

func (r *barRenderer) MinSize() fyne.Size { return r.bar.MinSize() }
func (r *barRenderer) Refresh() {
	r.track.FillColor = r.bar.track
	r.fg.FillColor = r.bar.color
	r.track.Refresh()
	r.fg.Refresh()
	r.Layout(r.bar.Size())
}
func (r *barRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.track, r.fg} }
func (r *barRenderer) Destroy()                     {}
