package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Palette matches the CSS variables in index.html so the Fyne UI keeps
// the same dark/cyan look as the Chromium kiosk dashboard.
var Palette = struct {
	Void, Deck, Panel, Panel2 color.RGBA
	Line, Line2               color.RGBA
	Text, Muted, Faint        color.RGBA
	Blue, Cyan, Violet        color.RGBA
	Green, Orange, Red        color.RGBA
	Amber, OK, Magenta        color.RGBA
}{
	Void:    color.RGBA{0x0d, 0x11, 0x17, 0xff},
	Deck:    color.RGBA{0x01, 0x04, 0x09, 0xff},
	Panel:   color.RGBA{0x16, 0x1b, 0x22, 0xff},
	Panel2:  color.RGBA{0x16, 0x1b, 0x22, 0xff},
	Line:    color.RGBA{0x30, 0x36, 0x3d, 0xff},
	Line2:   color.RGBA{0x21, 0x26, 0x2d, 0xff},
	Text:    color.RGBA{0xc9, 0xd1, 0xd9, 0xff},
	Muted:   color.RGBA{0x8b, 0x94, 0x9e, 0xff},
	Faint:   color.RGBA{0x6e, 0x76, 0x81, 0xff},
	Blue:    color.RGBA{0x58, 0xa6, 0xff, 0xff},
	Cyan:    color.RGBA{0x58, 0xa6, 0xff, 0xff},
	Violet:  color.RGBA{0xa3, 0x71, 0xf7, 0xff},
	Green:   color.RGBA{0x22, 0xc5, 0x5e, 0xff},
	Orange:  color.RGBA{0xf5, 0x9e, 0x0b, 0xff},
	Red:     color.RGBA{0xef, 0x44, 0x44, 0xff},
	Amber:   color.RGBA{0xd2, 0x99, 0x22, 0xff},
	OK:      color.RGBA{0x3f, 0xb9, 0x50, 0xff},
	Magenta: color.RGBA{0xa3, 0x71, 0xf7, 0xff},
}

// dashboardTheme implements fyne.Theme. We override only color and a
// handful of sizes; everything else falls back to the default dark theme.
type dashboardTheme struct{}

var _ fyne.Theme = (*dashboardTheme)(nil)

func NewTheme() fyne.Theme { return &dashboardTheme{} }

func (t *dashboardTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return Palette.Void
	case theme.ColorNameForeground:
		return Palette.Text
	case theme.ColorNameDisabled:
		return Palette.Faint
	case theme.ColorNameDisabledButton, theme.ColorNameButton:
		return Palette.Panel
	case theme.ColorNamePrimary, theme.ColorNameFocus:
		return Palette.Cyan
	case theme.ColorNameHover:
		return color.RGBA{0x58, 0xa6, 0xff, 0x22}
	case theme.ColorNameInputBackground, theme.ColorNameInputBorder:
		return Palette.Panel
	case theme.ColorNamePlaceHolder:
		return Palette.Muted
	case theme.ColorNameSeparator, theme.ColorNameShadow:
		return Palette.Line
	case theme.ColorNameSuccess:
		return Palette.OK
	case theme.ColorNameWarning:
		return Palette.Amber
	case theme.ColorNameError:
		return Palette.Magenta
	case theme.ColorNameOverlayBackground:
		return Palette.Deck
	case theme.ColorNameSelection:
		return color.RGBA{0x58, 0xa6, 0xff, 0x33}
	case theme.ColorNameMenuBackground:
		return Palette.Panel
	}
	return theme.DefaultTheme().Color(name, theme.VariantDark)
}

func (t *dashboardTheme) Font(s fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(fyne.TextStyle{Monospace: true, Bold: s.Bold, Italic: s.Italic})
}

func (t *dashboardTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *dashboardTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 3
	case theme.SizeNameInnerPadding:
		return 4
	case theme.SizeNameInlineIcon:
		return 14
	case theme.SizeNameText:
		return 12
	case theme.SizeNameCaptionText:
		return 10
	case theme.SizeNameSubHeadingText:
		return 14
	case theme.SizeNameHeadingText:
		return 16
	case theme.SizeNameSeparatorThickness:
		return 1
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameScrollBar, theme.SizeNameScrollBarSmall:
		return 6
	}
	return theme.DefaultTheme().Size(name)
}
