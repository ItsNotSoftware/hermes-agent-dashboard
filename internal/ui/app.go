package ui

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/diogo/hermes-agent-dashboard/internal/store"
)

// App owns the Fyne lifecycle for the dashboard. It subscribes to the
// store, refreshes the visible page on each snapshot update, and exits
// when its context is cancelled.
type App struct {
	fyne fyne.App
	win  fyne.Window
	st   *store.Store

	headerTitle *canvas.Text
	clockText   *canvas.Text
	modelTag    *widget.Label

	pages    []page
	tabs     []*pageTab
	pageRoot *fyne.Container
	current  int
}

// page is the contract every dashboard tab implements.
type page interface {
	Title() string
	Build() fyne.CanvasObject
	Update(snap store.Snapshot)
}

func New(st *store.Store) *App {
	a := app.NewWithID("hermes.dashboard")
	a.Settings().SetTheme(NewTheme())
	return &App{fyne: a, st: st}
}

func (a *App) Run(ctx context.Context) {
	a.win = a.fyne.NewWindow("Hermes Dashboard")
	a.win.SetPadded(false)
	a.win.Resize(fyne.NewSize(800, 480))
	a.win.SetFixedSize(true)

	a.pages = []page{
		newCorePage(),
		newDroidsPage(),
		newMissionPage(),
		newControlPage(),
	}

	a.pageRoot = container.NewStack()
	for i, p := range a.pages {
		obj := p.Build()
		if i != 0 {
			obj.Hide()
		}
		a.pageRoot.Add(obj)
	}

	a.win.SetContent(container.NewBorder(
		a.buildHeaderRow(),
		nil, nil, nil,
		container.NewBorder(a.buildTabBar(), nil, nil, nil, a.pageRoot),
	))

	go a.loop(ctx)
	go a.clockLoop(ctx)

	a.win.SetFullScreen(true)
	a.win.ShowAndRun()
}

// loop refreshes the visible page whenever the store emits an update.
// All Fyne mutations are marshalled to the UI thread via fyne.Do.
func (a *App) loop(ctx context.Context) {
	updates := a.st.Subscribe()
	snap := a.st.Snapshot()
	fyne.Do(func() { a.applySnapshot(snap) })
	for {
		select {
		case <-ctx.Done():
			fyne.Do(a.fyne.Quit)
			return
		case <-updates:
			snap := a.st.Snapshot()
			fyne.Do(func() { a.applySnapshot(snap) })
		}
	}
}

func (a *App) clockLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			ts := now.Format("15:04:05")
			fyne.Do(func() {
				a.clockText.Text = ts
				a.clockText.Refresh()
			})
		}
	}
}

func (a *App) applySnapshot(snap store.Snapshot) {
	model := snap.ModelInfo.Model
	if snap.ModelInfo.Provider != "" {
		model = fmt.Sprintf("%s · %s", snap.ModelInfo.Provider, model)
	}
	if model == "" {
		model = "—"
	}
	a.modelTag.SetText(strings.ToUpper(model))

	if a.current >= 0 && a.current < len(a.pages) {
		a.pages[a.current].Update(snap)
	}
}

func (a *App) buildHeaderRow() fyne.CanvasObject {
	a.headerTitle = canvas.NewText("HERMES // RPi DASHBOARD", Palette.Cyan)
	a.headerTitle.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	a.headerTitle.TextSize = 15

	a.clockText = canvas.NewText("--:--:--", Palette.Text)
	a.clockText.Alignment = fyne.TextAlignCenter
	a.clockText.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	a.clockText.TextSize = 18

	a.modelTag = widget.NewLabel("—")
	a.modelTag.TextStyle = fyne.TextStyle{Monospace: true}
	a.modelTag.Alignment = fyne.TextAlignTrailing

	quit := newQuitBtn(func() { fyne.Do(a.fyne.Quit) })

	row := container.NewBorder(nil, nil,
		container.NewHBox(a.headerTitle),
		container.NewHBox(a.modelTag, quit),
		container.NewCenter(a.clockText),
	)
	return panelWrap(row, 32)
}

func (a *App) buildTabBar() fyne.CanvasObject {
	a.tabs = make([]*pageTab, len(a.pages))
	objs := make([]fyne.CanvasObject, len(a.pages))
	for i, p := range a.pages {
		idx := i
		tab := newPageTab(p.Title(), i == 0, func() { a.showPage(idx) })
		a.tabs[i] = tab
		objs[i] = tab
	}
	return container.New(equalGrid(len(objs), 4), objs...)
}

func (a *App) showPage(i int) {
	if i == a.current || i < 0 || i >= len(a.pages) {
		return
	}
	a.pageRoot.Objects[a.current].Hide()
	a.pageRoot.Objects[i].Show()
	a.tabs[a.current].setActive(false)
	a.tabs[i].setActive(true)
	a.current = i
	a.pages[i].Update(a.st.Snapshot())
}

// panelWrap draws a thin bordered rounded panel behind a content object,
// approximating the .card / .header look from the original CSS.
func panelWrap(content fyne.CanvasObject, height float32) fyne.CanvasObject {
	bg := canvas.NewRectangle(Palette.Panel)
	bg.StrokeColor = Palette.Line
	bg.StrokeWidth = 1
	bg.CornerRadius = 6
	wrap := container.NewStack(bg, container.NewPadded(content))
	if height > 0 {
		wrap = container.NewStack(bg, container.New(&fixedHeight{h: height}, container.NewPadded(content)))
	}
	return wrap
}

// equalGrid lays out children in n equal columns with a fixed gap.
type equalGridLayout struct {
	cols int
	gap  float32
}

func equalGrid(cols int, gap float32) fyne.Layout { return &equalGridLayout{cols: cols, gap: gap} }

func (l *equalGridLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) == 0 || l.cols <= 0 {
		return
	}
	total := float32(l.cols)
	cellW := (size.Width - l.gap*(total-1)) / total
	x := float32(0)
	for _, o := range objs {
		o.Resize(fyne.NewSize(cellW, size.Height))
		o.Move(fyne.NewPos(x, 0))
		x += cellW + l.gap
	}
}

func (l *equalGridLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objs {
		m := o.MinSize()
		if m.Width > w {
			w = m.Width
		}
		if m.Height > h {
			h = m.Height
		}
	}
	w = w*float32(l.cols) + l.gap*float32(l.cols-1)
	return fyne.NewSize(w, h)
}

// fixedHeight forces its single child to a height, full width.
type fixedHeight struct{ h float32 }

func (f *fixedHeight) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(fyne.NewSize(size.Width, f.h))
		o.Move(fyne.NewPos(0, 0))
	}
}

func (f *fixedHeight) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w float32
	for _, o := range objs {
		if m := o.MinSize().Width; m > w {
			w = m
		}
	}
	return fyne.NewSize(w, f.h)
}

// pageTab is a clickable tab in the pager row.
type pageTab struct {
	widget.BaseWidget
	label  string
	active bool
	onTap  func()
}

func newPageTab(label string, active bool, onTap func()) *pageTab {
	t := &pageTab{label: strings.ToUpper(label), active: active, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *pageTab) setActive(v bool) {
	t.active = v
	t.Refresh()
}

func (t *pageTab) Tapped(_ *fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *pageTab) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(Palette.Panel)
	bg.StrokeColor = Palette.Line
	bg.StrokeWidth = 1
	bg.CornerRadius = 6
	txt := canvas.NewText(t.label, Palette.Muted)
	txt.Alignment = fyne.TextAlignCenter
	txt.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	txt.TextSize = 12
	dot := canvas.NewCircle(Palette.Faint)
	r := &tabRenderer{tab: t, bg: bg, txt: txt, dot: dot}
	r.apply()
	return r
}

type tabRenderer struct {
	tab *pageTab
	bg  *canvas.Rectangle
	txt *canvas.Text
	dot *canvas.Circle
}

func (r *tabRenderer) apply() {
	if r.tab.active {
		r.bg.FillColor = color.RGBA{0x58, 0xa6, 0xff, 0x20}
		r.bg.StrokeColor = color.RGBA{0x58, 0xa6, 0xff, 0x88}
		r.txt.Color = Palette.Text
		r.dot.FillColor = Palette.Cyan
	} else {
		r.bg.FillColor = Palette.Panel
		r.bg.StrokeColor = Palette.Line
		r.txt.Color = Palette.Muted
		r.dot.FillColor = Palette.Faint
	}
}

func (r *tabRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))
	dotSize := float32(6)
	r.dot.Resize(fyne.NewSize(dotSize, dotSize))
	r.dot.Move(fyne.NewPos(10, (size.Height-dotSize)/2))
	r.txt.Resize(size)
	r.txt.Move(fyne.NewPos(0, 0))
}

func (r *tabRenderer) MinSize() fyne.Size              { return fyne.NewSize(80, 26) }
func (r *tabRenderer) Refresh()                        { r.apply(); r.bg.Refresh(); r.txt.Refresh(); r.dot.Refresh() }
func (r *tabRenderer) Objects() []fyne.CanvasObject    { return []fyne.CanvasObject{r.bg, r.dot, r.txt} }
func (r *tabRenderer) Destroy()                        {}

// quitBtn is a small ✕ button for the header bar.
type quitBtn struct {
	widget.BaseWidget
	onTap func()
}

func newQuitBtn(onTap func()) *quitBtn {
	b := &quitBtn{onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *quitBtn) Tapped(_ *fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *quitBtn) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.RGBA{0x2c, 0x0d, 0x0d, 0xff})
	bg.StrokeColor = color.RGBA{0xef, 0x44, 0x44, 0xaa}
	bg.StrokeWidth = 1
	bg.CornerRadius = 7
	txt := canvas.NewText("✕", color.RGBA{0xff, 0x66, 0x66, 0xff})
	txt.Alignment = fyne.TextAlignCenter
	txt.TextStyle = fyne.TextStyle{Bold: true}
	txt.TextSize = 15
	return &quitRenderer{bg: bg, txt: txt}
}

type quitRenderer struct {
	bg  *canvas.Rectangle
	txt *canvas.Text
}

func (r *quitRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))
	r.txt.Resize(size)
	r.txt.Move(fyne.NewPos(0, (size.Height-r.txt.MinSize().Height)/2))
}
func (r *quitRenderer) MinSize() fyne.Size              { return fyne.NewSize(28, 24) }
func (r *quitRenderer) Refresh()                        { r.bg.Refresh(); r.txt.Refresh() }
func (r *quitRenderer) Objects() []fyne.CanvasObject    { return []fyne.CanvasObject{r.bg, r.txt} }
func (r *quitRenderer) Destroy()                        {}
