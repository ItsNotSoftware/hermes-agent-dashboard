// Package widgets contains the small reusable UI building blocks used
// across the dashboard pages (cards, gauges, sparklines, etc.).
package widgets

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// Palette colors needed across widgets are pinned here so the widgets
// package does not import the parent ui package (avoids an import cycle).
var (
	ColPanel   = color.RGBA{0x16, 0x1b, 0x22, 0xff}
	ColLine    = color.RGBA{0x30, 0x36, 0x3d, 0xff}
	ColText    = color.RGBA{0xc9, 0xd1, 0xd9, 0xff}
	ColMuted   = color.RGBA{0x8b, 0x94, 0x9e, 0xff}
	ColFaint   = color.RGBA{0x6e, 0x76, 0x81, 0xff}
	ColCyan    = color.RGBA{0x58, 0xa6, 0xff, 0xff}
	ColViolet  = color.RGBA{0xa3, 0x71, 0xf7, 0xff}
	ColGreen   = color.RGBA{0x22, 0xc5, 0x5e, 0xff}
	ColAmber   = color.RGBA{0xd2, 0x99, 0x22, 0xff}
	ColOrange  = color.RGBA{0xf5, 0x9e, 0x0b, 0xff}
	ColRed     = color.RGBA{0xef, 0x44, 0x44, 0xff}
	ColOK      = color.RGBA{0x3f, 0xb9, 0x50, 0xff}
)

// Card wraps content in a bordered rounded panel with an optional title.
func Card(title string, content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(ColPanel)
	bg.StrokeColor = ColLine
	bg.StrokeWidth = 1
	bg.CornerRadius = 6

	body := content
	if title != "" {
		t := canvas.NewText(strings.ToUpper(title), ColMuted)
		t.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
		t.TextSize = 10
		body = container.NewBorder(t, nil, nil, nil, content)
	}
	return container.NewStack(bg, container.NewPadded(body))
}

// SeverityColor maps a severity label to a foreground color used by the
// dashboard's status pills, log entries, and command list rows.
func SeverityColor(sev string) color.Color {
	switch sev {
	case "ok":
		return ColOK
	case "warn":
		return ColAmber
	case "critical":
		return ColViolet
	case "paused":
		return ColAmber
	case "failed":
		return ColRed
	case "idle":
		return ColFaint
	}
	return ColMuted
}

// Label is a quick helper for monospaced canvas text.
func Label(s string, col color.Color, size float32, bold bool) *canvas.Text {
	t := canvas.NewText(s, col)
	t.TextStyle = fyne.TextStyle{Monospace: true, Bold: bold}
	t.TextSize = size
	return t
}
