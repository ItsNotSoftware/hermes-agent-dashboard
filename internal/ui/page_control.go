package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"github.com/diogo/hermes-agent-dashboard/internal/derive"
	"github.com/diogo/hermes-agent-dashboard/internal/loops"
	"github.com/diogo/hermes-agent-dashboard/internal/store"
	"github.com/diogo/hermes-agent-dashboard/internal/ui/widgets"
)

type controlPage struct {
	calendar *fyne.Container
	todos    *fyne.Container
	loops    *fyne.Container
	inbox    *fyne.Container
	commands *fyne.Container
}

func newControlPage() *controlPage    { return &controlPage{} }
func (p *controlPage) Title() string  { return "Control" }

func (p *controlPage) Build() fyne.CanvasObject {
	p.calendar = container.NewVBox()
	p.todos = container.NewVBox()
	p.loops = container.NewVBox()
	p.inbox = container.NewVBox()
	p.commands = container.NewVBox()

	loopsCard := widgets.Card("TODOS / OPEN LOOPS", container.NewVBox(
		widgets.Label("TASKS", widgets.ColMuted, 10, true),
		container.NewScroll(p.todos),
		widgets.Label("LOOPS", widgets.ColMuted, 10, true),
		container.NewScroll(p.loops),
		widgets.Label("INBOX", widgets.ColMuted, 10, true),
		container.NewScroll(p.inbox),
	))

	return container.New(equalGrid(3, 6),
		widgets.Card("UPCOMING CALENDAR", container.NewScroll(p.calendar)),
		loopsCard,
		widgets.Card("MISSION COMMANDS", container.NewVBox(p.commands)),
	)
}

func (p *controlPage) Update(snap store.Snapshot) {
	p.calendar.Objects = nil
	for _, ev := range snap.Calendar.Events {
		line1 := widgets.Label(ev.Day+"  "+ev.Clock, widgets.ColCyan, 11, true)
		line2 := widgets.Label(ev.Title, widgets.ColText, 12, false)
		p.calendar.Add(container.NewVBox(line1, line2))
	}
	if len(snap.Calendar.Events) == 0 {
		msg := "no upcoming events"
		if snap.Calendar.Status != "ok" {
			msg = strings.ToUpper(snap.Calendar.Status)
			if snap.Calendar.Error != "" {
				msg += ": " + snap.Calendar.Error
			}
		}
		p.calendar.Add(widgets.Label(msg, widgets.ColFaint, 12, false))
	}
	p.calendar.Refresh()

	fillItems(p.todos, snap.Loops.Todos, "no tasks")
	fillItems(p.loops, snap.Loops.OpenLoops, "no loops")
	fillItems(p.inbox, snap.Loops.Inbox, "inbox clear")

	p.commands.Objects = nil
	for _, cmd := range snap.MissionControl.Commands {
		p.commands.Add(commandRow(cmd))
	}
	p.commands.Refresh()
}

func fillItems(c *fyne.Container, items []loops.Item, empty string) {
	c.Objects = nil
	for _, it := range items {
		c.Add(container.NewHBox(
			widgets.Label("• ", widgets.ColMuted, 12, false),
			widgets.Label(it.Title, widgets.ColText, 12, false),
			widgets.Label(it.Source, widgets.ColFaint, 10, false),
		))
	}
	if len(items) == 0 {
		c.Add(widgets.Label(empty, widgets.ColFaint, 11, false))
	}
	c.Refresh()
}

func commandRow(c derive.Command) fyne.CanvasObject {
	state := widgets.Label(strings.ToUpper(c.State), widgets.SeverityColor(c.State), 9, true)
	title := widgets.Label(c.Title, widgets.ColText, 12, true)
	detail := widgets.Label(c.Detail, widgets.ColMuted, 11, false)
	return container.NewVBox(container.NewHBox(state, title), detail)
}
