package ui

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	"github.com/diogo/hermes-agent-dashboard/internal/claude"
	"github.com/diogo/hermes-agent-dashboard/internal/ui/widgets"
)

func pctColor(p float64) color.Color {
	switch {
	case p < 50:
		return widgets.ColGreen
	case p < 80:
		return widgets.ColOrange
	default:
		return widgets.ColRed
	}
}

func tempColor(t float64) color.Color {
	switch {
	case t < 60:
		return widgets.ColOK
	case t < 75:
		return widgets.ColAmber
	default:
		return widgets.ColViolet
	}
}

func humanRate(bps float64) string {
	switch {
	case bps >= 1<<30:
		return fmt.Sprintf("%.1f GB/s", bps/(1<<30))
	case bps >= 1<<20:
		return fmt.Sprintf("%.1f MB/s", bps/(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.1f kB/s", bps/(1<<10))
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

func formatUptime(seconds float64) string {
	d := int(seconds) / 86400
	h := (int(seconds) % 86400) / 3600
	m := (int(seconds) % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func pctText(w *claude.Window) string {
	if w == nil || w.UsedPercent == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *w.UsedPercent)
}

func refreshAll(objs ...fyne.CanvasObject) {
	for _, o := range objs {
		if o == nil {
			continue
		}
		if t, ok := o.(*canvas.Text); ok {
			t.Refresh()
			continue
		}
		o.Refresh()
	}
}
