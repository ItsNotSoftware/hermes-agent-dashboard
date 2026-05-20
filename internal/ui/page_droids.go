package ui

import (
	"fmt"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"github.com/diogo/hermes-agent-dashboard/internal/derive"
	"github.com/diogo/hermes-agent-dashboard/internal/store"
	"github.com/diogo/hermes-agent-dashboard/internal/ui/widgets"
)

type droidsPage struct {
	roster   *fyne.Container
	cron     *fyne.Container
	kpiJobs  *canvas.Text
	kpiPaused *canvas.Text
	kpiFailed *canvas.Text
	deckState *canvas.Text
	warnings *fyne.Container
}

func newDroidsPage() *droidsPage { return &droidsPage{} }

func (p *droidsPage) Title() string { return "Droids" }

func (p *droidsPage) Build() fyne.CanvasObject {
	p.roster = container.NewVBox()
	p.cron = container.NewVBox()
	p.warnings = container.NewVBox()
	p.kpiJobs = widgets.Label("0", widgets.ColCyan, 24, true)
	p.kpiPaused = widgets.Label("0", widgets.ColAmber, 24, true)
	p.kpiFailed = widgets.Label("0", widgets.ColRed, 24, true)
	p.deckState = widgets.Label("idle", widgets.ColMuted, 12, true)

	kpiRow := container.New(equalGrid(3, 6),
		kpi("TOTAL", p.kpiJobs), kpi("PAUSED", p.kpiPaused), kpi("FAILED", p.kpiFailed))
	commandDeck := widgets.Card("COMMAND DECK", container.NewVBox(kpiRow, p.deckState))

	left := container.New(rowsLayout{rows: 2, gap: 6},
		widgets.Card("DROID ROSTER", p.roster),
		commandDeck,
	)
	right := container.New(rowsLayout{rows: 2, gap: 6},
		widgets.Card("CRON SCHEDULE", container.NewScroll(p.cron)),
		widgets.Card("ATTENTION", container.NewScroll(p.warnings)),
	)
	return container.New(equalGrid(2, 6), left, right)
}

func kpi(label string, value *canvas.Text) fyne.CanvasObject {
	lab := widgets.Label(label, widgets.ColMuted, 10, true)
	return container.NewVBox(value, lab)
}

func (p *droidsPage) Update(snap store.Snapshot) {
	jobs := 0
	paused := 0
	failed := 0
	for _, c := range snap.Crons {
		jobs++
		if c.State == "paused" {
			paused++
		}
		if c.LastStatus != "" && c.LastStatus != "ok" {
			failed++
		}
	}
	p.kpiJobs.Text = fmt.Sprintf("%d", jobs)
	p.kpiPaused.Text = fmt.Sprintf("%d", paused)
	p.kpiFailed.Text = fmt.Sprintf("%d", failed)

	state := "idle"
	switch {
	case failed > 0:
		state = "failed"
	case paused > 0:
		state = "paused"
	case jobs > 0:
		state = "ok"
	}
	p.deckState.Text = strings.ToUpper(state)
	p.deckState.Color = widgets.SeverityColor(state)

	p.roster.Objects = nil
	names := make([]string, 0, len(snap.AgentOps))
	for k := range snap.AgentOps {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		p.roster.Add(rosterRow(snap.AgentOps[n]))
	}
	if len(names) == 0 {
		p.roster.Add(widgets.Label("no droids configured", widgets.ColFaint, 12, false))
	}

	p.cron.Objects = nil
	for _, c := range snap.Crons {
		p.cron.Add(cronRow(c.Name, c.Schedule, c.NextRun, c.Owner, c.State))
	}
	if len(snap.Crons) == 0 {
		p.cron.Add(widgets.Label("no jobs scheduled", widgets.ColFaint, 12, false))
	}

	p.warnings.Objects = nil
	for _, ev := range snap.MissionLog {
		if ev.Severity == "ok" {
			continue
		}
		p.warnings.Add(warningRow(ev))
	}
	if len(p.warnings.Objects) == 0 {
		p.warnings.Add(widgets.Label("no alerts", widgets.ColFaint, 12, false))
	}

	p.roster.Refresh()
	p.cron.Refresh()
	p.warnings.Refresh()
	refreshAll(p.kpiJobs, p.kpiPaused, p.kpiFailed, p.deckState)
}

func rosterRow(ops derive.AgentOps) fyne.CanvasObject {
	name := widgets.Label(strings.ToUpper(ops.Name), widgets.ColText, 12, true)
	state := widgets.Label(strings.ToUpper(ops.State), widgets.SeverityColor(ops.State), 10, true)
	jobs := widgets.Label(fmt.Sprintf("%d jobs", ops.JobCount), widgets.ColMuted, 10, false)
	next := "—"
	if ops.NextJob != nil {
		next = ops.NextJob.Name + " @ " + ops.NextJob.NextRun
	}
	return container.NewVBox(
		container.NewHBox(name, state, jobs),
		widgets.Label(next, widgets.ColMuted, 11, false),
	)
}

func cronRow(name, schedule, next, owner, state string) fyne.CanvasObject {
	col := widgets.ColText
	if state == "paused" {
		col = widgets.ColAmber
	}
	title := widgets.Label(name, col, 12, true)
	sub := widgets.Label(
		fmt.Sprintf("%s · %s · next %s", strings.ToUpper(owner), schedule, next),
		widgets.ColMuted, 10, false)
	return container.NewVBox(title, sub)
}

func warningRow(ev derive.MissionEvent) fyne.CanvasObject {
	label := widgets.Label(ev.Label, widgets.SeverityColor(ev.Severity), 10, true)
	title := widgets.Label(ev.Title, widgets.ColText, 12, true)
	detail := widgets.Label(ev.Detail, widgets.ColMuted, 11, false)
	return container.NewVBox(container.NewHBox(label, title), detail)
}
