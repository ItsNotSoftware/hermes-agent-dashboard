package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"github.com/diogo/hermes-agent-dashboard/internal/store"
	"github.com/diogo/hermes-agent-dashboard/internal/ui/widgets"
)

type missionPage struct {
	log         *fyne.Container
	service     *canvas.Text
	host, ip    *canvas.Text
	kernel, py  *canvas.Text
	uptime      *canvas.Text
	load        *canvas.Text
	procs       *canvas.Text
	threads     *canvas.Text
	thermal     *canvas.Text
	io          *canvas.Text
	repo        *canvas.Text
	procList    *fyne.Container
}

func newMissionPage() *missionPage { return &missionPage{} }
func (p *missionPage) Title() string { return "Mission" }

func (p *missionPage) Build() fyne.CanvasObject {
	p.log = container.NewVBox()
	p.service = widgets.Label("—", widgets.ColText, 12, false)
	p.host = widgets.Label("—", widgets.ColText, 13, true)
	p.ip = widgets.Label("—", widgets.ColText, 13, true)
	p.kernel = widgets.Label("—", widgets.ColMuted, 11, false)
	p.py = widgets.Label("—", widgets.ColMuted, 11, false)

	p.uptime = widgets.Label("—", widgets.ColText, 12, false)
	p.load = widgets.Label("—", widgets.ColText, 12, false)
	p.procs = widgets.Label("—", widgets.ColText, 12, false)
	p.threads = widgets.Label("—", widgets.ColText, 12, false)
	p.thermal = widgets.Label("—", widgets.ColText, 12, false)
	p.io = widgets.Label("—", widgets.ColText, 12, false)
	p.repo = widgets.Label("—", widgets.ColMuted, 11, false)

	p.procList = container.NewVBox()

	nodeCard := widgets.Card("NODE", container.NewVBox(
		container.NewHBox(widgets.Label("HOST", widgets.ColMuted, 10, true), p.host),
		container.NewHBox(widgets.Label("IP  ", widgets.ColMuted, 10, true), p.ip),
		p.kernel, p.py, p.repo,
	))
	consoleCard := widgets.Card("CAPTAIN CONSOLE", container.NewVBox(
		kv("UPTIME", p.uptime), kv("LOAD", p.load),
		kv("PROCS", p.procs), kv("THREADS", p.threads),
		kv("THERMAL", p.thermal), kv("I/O", p.io),
	))
	serviceCard := widgets.Card("SERVICE / NETWORK", container.NewVBox(p.service))

	right := container.New(rowsLayout{rows: 3, gap: 6}, nodeCard, consoleCard, serviceCard)
	left := container.New(rowsLayout{rows: 2, gap: 6},
		widgets.Card("MISSION LOG", container.NewScroll(p.log)),
		widgets.Card("TOP PROCESSES", container.NewScroll(p.procList)),
	)
	return container.New(equalGrid(2, 6), left, right)
}

func kv(label string, val *canvas.Text) fyne.CanvasObject {
	return container.NewHBox(widgets.Label(label, widgets.ColMuted, 10, true), val)
}

func (p *missionPage) Update(snap store.Snapshot) {
	p.log.Objects = nil
	for _, ev := range snap.MissionLog {
		p.log.Add(warningRow(ev))
	}
	if len(snap.MissionLog) == 0 {
		p.log.Add(widgets.Label("no recent events", widgets.ColFaint, 12, false))
	}
	p.log.Refresh()

	p.host.Text = snap.Hostname
	p.ip.Text = snap.ServiceHealth.LocalIP
	p.kernel.Text = snap.Kernel
	p.py.Text = snap.Python
	if snap.Repo.Branch != "" {
		dirty := ""
		if snap.Repo.Dirty {
			dirty = fmt.Sprintf(" (%d dirty)", snap.Repo.ChangedFiles)
		}
		p.repo.Text = fmt.Sprintf("git %s @ %s%s", snap.Repo.Branch, snap.Repo.Head, dirty)
	} else {
		p.repo.Text = ""
	}

	p.uptime.Text = formatUptime(snap.Uptime)
	p.load.Text = fmt.Sprintf("%.2f %.2f %.2f", snap.Load[0], snap.Load[1], snap.Load[2])
	p.procs.Text = fmt.Sprintf("%d", snap.ProcCount)
	p.threads.Text = fmt.Sprintf("%d", snap.Threads)
	p.thermal.Text = fmt.Sprintf("%.1f°C · %s", snap.Temp, snap.Power.Summary)
	p.io.Text = fmt.Sprintf("R %s · W %s", humanRate(snap.DiskRd), humanRate(snap.DiskWt))

	internet := "offline"
	if snap.ServiceHealth.Internet.Reachable {
		if snap.ServiceHealth.Internet.LatencyMs != nil {
			internet = fmt.Sprintf("online · %.1f ms", *snap.ServiceHealth.Internet.LatencyMs)
		} else {
			internet = "online"
		}
	}
	fresh := "—"
	if snap.ServiceHealth.HermesCron.FreshnessSeconds != nil {
		fresh = fmt.Sprintf("cron %ds", *snap.ServiceHealth.HermesCron.FreshnessSeconds)
	}
	p.service.Text = fmt.Sprintf("internet: %s   %s", internet, fresh)

	p.procList.Objects = nil
	for _, pr := range snap.Procs {
		line := widgets.Label(
			fmt.Sprintf("%-5s %5.1f%% cpu  %4.1f%% mem  %s", pr.PID, pr.CPU, pr.Mem, pr.Name),
			widgets.ColText, 10, false)
		p.procList.Add(line)
	}
	p.procList.Refresh()

	refreshAll(p.host, p.ip, p.kernel, p.py, p.repo, p.uptime, p.load,
		p.procs, p.threads, p.thermal, p.io, p.service)
}
