package ui

import "fyne.io/fyne/v2"

// rowsLayout stacks its children vertically, sharing height equally.
type rowsLayout struct {
	rows int
	gap  float32
}

func (l rowsLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) == 0 {
		return
	}
	n := l.rows
	if n <= 0 {
		n = len(objs)
	}
	rowH := (size.Height - l.gap*float32(n-1)) / float32(n)
	y := float32(0)
	for _, o := range objs {
		o.Resize(fyne.NewSize(size.Width, rowH))
		o.Move(fyne.NewPos(0, y))
		y += rowH + l.gap
	}
}

func (l rowsLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objs {
		m := o.MinSize()
		if m.Width > w {
			w = m.Width
		}
		h += m.Height
	}
	h += l.gap * float32(len(objs)-1)
	return fyne.NewSize(w, h)
}

// bottomFixed: 2 children — first fills all available space, second is pinned
// at the bottom using its own MinSize height. More reliable than NewBorder for
// this pattern because it never produces negative heights.
type bottomFixed struct{ gap float32 }

func (l bottomFixed) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) < 2 {
		return
	}
	bh := objs[1].MinSize().Height
	y2 := size.Height - bh
	if y2 < 0 {
		y2 = 0
		bh = size.Height
	}
	th := y2 - l.gap
	if th < 0 {
		th = 0
	}
	objs[0].Resize(fyne.NewSize(size.Width, th))
	objs[0].Move(fyne.NewPos(0, 0))
	objs[1].Resize(fyne.NewSize(size.Width, bh))
	objs[1].Move(fyne.NewPos(0, y2))
}
func (l bottomFixed) MinSize(objs []fyne.CanvasObject) fyne.Size {
	if len(objs) < 2 {
		return fyne.NewSize(0, 0)
	}
	w := objs[0].MinSize().Width
	if w2 := objs[1].MinSize().Width; w2 > w {
		w = w2
	}
	return fyne.NewSize(w, objs[0].MinSize().Height+l.gap+objs[1].MinSize().Height)
}

// leftColLayout: 2 children — left child gets a fixed width, right child fills.
type leftColLayout struct{ leftW, gap float32 }

func (l leftColLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) == 0 {
		return
	}
	h := size.Height
	objs[0].Resize(fyne.NewSize(l.leftW, h))
	objs[0].Move(fyne.NewPos(0, 0))
	if len(objs) > 1 {
		x := l.leftW + l.gap
		objs[1].Resize(fyne.NewSize(size.Width-x, h))
		objs[1].Move(fyne.NewPos(x, 0))
	}
}
func (l leftColLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		if m := o.MinSize().Height; m > h {
			h = m
		}
	}
	w := l.leftW + l.gap
	if len(objs) > 1 {
		w += objs[1].MinSize().Width
	}
	return fyne.NewSize(w, h)
}

// triColLayout: 3 children — left and right get fixed widths, center fills.
// Text children are vertically centred; bar/widget children get full height.
type triColLayout struct{ lW, rW, gap float32 }

func (l triColLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) < 3 {
		return
	}
	h := size.Height
	// left
	lh := objs[0].MinSize().Height
	objs[0].Resize(fyne.NewSize(l.lW, lh))
	objs[0].Move(fyne.NewPos(0, (h-lh)/2))
	// right
	rh := objs[2].MinSize().Height
	objs[2].Resize(fyne.NewSize(l.rW, rh))
	objs[2].Move(fyne.NewPos(size.Width-l.rW, (h-rh)/2))
	// center fills
	cx := l.lW + l.gap
	cw := size.Width - l.lW - l.rW - 3*l.gap
	if cw < 0 {
		cw = 0
	}
	objs[1].Resize(fyne.NewSize(cw, h))
	objs[1].Move(fyne.NewPos(cx, 0))
}
func (l triColLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		if m := o.MinSize().Height; m > h {
			h = m
		}
	}
	return fyne.NewSize(l.lW+l.rW+3*l.gap, h)
}

// quadColLayout: 4 children — col1 left-fixed, col2 flex, col3+col4 right-fixed.
type quadColLayout struct{ c1W, c3W, c4W, gap float32 }

func (l quadColLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) < 4 {
		return
	}
	h := size.Height
	// col1
	c1h := objs[0].MinSize().Height
	objs[0].Resize(fyne.NewSize(l.c1W, c1h))
	objs[0].Move(fyne.NewPos(0, (h-c1h)/2))
	// col3
	x3 := size.Width - l.c3W - l.gap - l.c4W
	c3h := objs[2].MinSize().Height
	objs[2].Resize(fyne.NewSize(l.c3W, c3h))
	objs[2].Move(fyne.NewPos(x3, (h-c3h)/2))
	// col4
	x4 := size.Width - l.c4W
	c4h := objs[3].MinSize().Height
	objs[3].Resize(fyne.NewSize(l.c4W, c4h))
	objs[3].Move(fyne.NewPos(x4, (h-c4h)/2))
	// col2 fills
	x2 := l.c1W + l.gap
	w2 := x3 - l.gap - x2
	if w2 < 0 {
		w2 = 0
	}
	objs[1].Resize(fyne.NewSize(w2, h))
	objs[1].Move(fyne.NewPos(x2, 0))
}
func (l quadColLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		if m := o.MinSize().Height; m > h {
			h = m
		}
	}
	return fyne.NewSize(l.c1W+l.c3W+l.c4W+4*l.gap, h)
}
