package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"github.com/diogo/hermes-agent-dashboard/internal/claude"
	"github.com/diogo/hermes-agent-dashboard/internal/store"
	"github.com/diogo/hermes-agent-dashboard/internal/ui/widgets"
)

const (
	numCores = 4
	numProcs = 5
)

// colTrack is the faint bar background colour.
var colTrack = color.RGBA{0x30, 0x36, 0x3d, 0x80}

type corePage struct {
	// CPU gauge
	cpuRing  *widgets.Ring
	cpuPct   *canvas.Text
	tempChip *canvas.Text
	freqChip *canvas.Text

	// Per-core bars
	coreBar [numCores]*widgets.Bar
	corePct [numCores]*canvas.Text

	// Resource bars
	ramBar     *widgets.Bar
	ramPct     *canvas.Text
	ramDetail  *canvas.Text
	diskBar    *widgets.Bar
	diskPct    *canvas.Text
	diskDetail *canvas.Text

	// AI – OpenAI
	gptPill   *canvas.Text
	gpt5hBar  *widgets.Bar
	gpt5hPct  *canvas.Text
	gpt5hMeta *canvas.Text
	gpt1wBar  *widgets.Bar
	gpt1wPct  *canvas.Text
	gpt1wMeta *canvas.Text

	// AI – Claude
	clPill   *canvas.Text
	cl5hBar  *widgets.Bar
	cl5hPct  *canvas.Text
	cl5hMeta *canvas.Text
	cl1wBar  *widgets.Bar
	cl1wPct  *canvas.Text
	cl1wMeta *canvas.Text

	// Proc table: row 0 = data rows 1..5
	procPID  [numProcs]*canvas.Text
	procName [numProcs]*canvas.Text
	procCPU  [numProcs]*canvas.Text
	procMem  [numProcs]*canvas.Text
}

func newCorePage() *corePage { return &corePage{} }
func (p *corePage) Title() string { return "Core" }

func (p *corePage) Build() fyne.CanvasObject {
	mono := fyne.TextStyle{Monospace: true}
	bold := fyne.TextStyle{Monospace: true, Bold: true}
	lbl := func(s string, col color.Color, sz float32) *canvas.Text {
		t := canvas.NewText(s, col)
		t.TextStyle = mono
		t.TextSize = sz
		return t
	}
	blbl := func(s string, col color.Color, sz float32) *canvas.Text {
		t := canvas.NewText(s, col)
		t.TextStyle = bold
		t.TextSize = sz
		return t
	}

	// ── CPU gauge ──────────────────────────────────────────
	p.cpuRing = widgets.NewRing(0, widgets.ColOK, color.RGBA{0x21, 0x26, 0x2d, 0xff})
	p.cpuPct = blbl("--%", widgets.ColOK, 16)
	p.tempChip = lbl("TMP --°", widgets.ColOK, 11)
	p.freqChip = lbl("CLK --", widgets.ColCyan, 11)
	cpuLbl := lbl("CPU", widgets.ColMuted, 9)

	gaugeStack := container.NewStack(p.cpuRing, container.NewCenter(p.cpuPct))
	gaugeCol := container.NewVBox(
		gaugeStack,
		container.NewCenter(cpuLbl),
		p.tempChip,
		p.freqChip,
	)

	// ── Core bars ──────────────────────────────────────────
	coreRows := make([]fyne.CanvasObject, numCores)
	for i := 0; i < numCores; i++ {
		cl := lbl(fmt.Sprintf("C%d", i), widgets.ColMuted, 11)
		p.coreBar[i] = widgets.NewBar(0, widgets.ColOK, colTrack, 9)
		p.corePct[i] = blbl(" 0%", widgets.ColOK, 11)
		coreRows[i] = container.New(triColLayout{18, 32, 5}, cl, p.coreBar[i], p.corePct[i])
	}
	coresArea := container.New(rowsLayout{rows: numCores, gap: 3}, coreRows...)

	// ── Resource bars ──────────────────────────────────────
	ramLbl := lbl("RAM", widgets.ColMuted, 11)
	p.ramBar = widgets.NewBar(0, widgets.ColOK, colTrack, 9)
	p.ramPct = blbl("--%", widgets.ColOK, 11)
	p.ramDetail = lbl("-- / --", widgets.ColMuted, 9)
	ramRow := container.New(quadColLayout{26, 32, 56, 4}, ramLbl, p.ramBar, p.ramPct, p.ramDetail)

	diskLbl := lbl("DISK", widgets.ColMuted, 11)
	p.diskBar = widgets.NewBar(0, widgets.ColOK, colTrack, 9)
	p.diskPct = blbl("--%", widgets.ColOK, 11)
	p.diskDetail = lbl("-- / --", widgets.ColMuted, 9)
	diskRow := container.New(quadColLayout{26, 32, 56, 4}, diskLbl, p.diskBar, p.diskPct, p.diskDetail)

	resArea := container.New(rowsLayout{rows: 2, gap: 4}, ramRow, diskRow)

	// CPU card: gauge+cores on top, resource bars at bottom
	topCPU := container.New(leftColLayout{86, 10}, gaugeCol, coresArea)
	cpuContent := container.NewBorder(nil, resArea, nil, nil, topCPU)

	// ── AI Usage – GPT ─────────────────────────────────────
	gptName := blbl("GPT", widgets.ColCyan, 12)
	p.gptPill = lbl("--", widgets.ColMuted, 9)
	gptHeader := container.NewBorder(nil, nil, nil, p.gptPill, gptName)

	gpt5hTag := lbl("5h", widgets.ColMuted, 9)
	p.gpt5hBar = widgets.NewBar(0, widgets.ColCyan, color.RGBA{0x1a, 0x28, 0x3a, 0xff}, 12)
	p.gpt5hPct = blbl("--%", widgets.ColCyan, 12)
	p.gpt5hMeta = lbl("--", widgets.ColMuted, 9)
	gpt5hRow := container.New(quadColLayout{18, 32, 58, 4}, gpt5hTag, p.gpt5hBar, p.gpt5hPct, p.gpt5hMeta)

	gpt1wTag := lbl("1w", widgets.ColMuted, 9)
	p.gpt1wBar = widgets.NewBar(0, color.RGBA{0x58, 0x80, 0xff, 0xff}, color.RGBA{0x1a, 0x1e, 0x38, 0xff}, 12)
	p.gpt1wPct = blbl("--%", color.RGBA{0x90, 0xa8, 0xff, 0xff}, 12)
	p.gpt1wMeta = lbl("--", widgets.ColMuted, 9)
	gpt1wRow := container.New(quadColLayout{18, 32, 58, 4}, gpt1wTag, p.gpt1wBar, p.gpt1wPct, p.gpt1wMeta)

	gptPanel := aiProvider(gptHeader, container.New(rowsLayout{rows: 2, gap: 5}, gpt5hRow, gpt1wRow))

	// ── AI Usage – Claude ──────────────────────────────────
	clName := blbl("CLAUDE", widgets.ColViolet, 12)
	p.clPill = lbl("--", widgets.ColMuted, 9)
	clHeader := container.NewBorder(nil, nil, nil, p.clPill, clName)

	cl5hTag := lbl("5h", widgets.ColMuted, 9)
	p.cl5hBar = widgets.NewBar(0, widgets.ColViolet, color.RGBA{0x28, 0x18, 0x38, 0xff}, 12)
	p.cl5hPct = blbl("--", widgets.ColViolet, 12)
	p.cl5hMeta = lbl("--", widgets.ColMuted, 9)
	cl5hRow := container.New(quadColLayout{18, 32, 58, 4}, cl5hTag, p.cl5hBar, p.cl5hPct, p.cl5hMeta)

	cl1wTag := lbl("1w", widgets.ColMuted, 9)
	p.cl1wBar = widgets.NewBar(0, widgets.ColCyan, color.RGBA{0x18, 0x1e, 0x38, 0xff}, 12)
	p.cl1wPct = blbl("--", widgets.ColCyan, 12)
	p.cl1wMeta = lbl("--", widgets.ColMuted, 9)
	cl1wRow := container.New(quadColLayout{18, 32, 58, 4}, cl1wTag, p.cl1wBar, p.cl1wPct, p.cl1wMeta)

	clPanel := aiProvider(clHeader, container.New(rowsLayout{rows: 2, gap: 5}, cl5hRow, cl1wRow))

	aiContent := container.New(rowsLayout{rows: 2, gap: 6}, gptPanel, clPanel)

	// ── Proc table ─────────────────────────────────────────
	procLayout := quadColLayout{38, 38, 38, 5}
	headPID := lbl("PID", widgets.ColMuted, 10)
	headCmd := lbl("CMD", widgets.ColMuted, 10)
	headCPU := lbl("CPU", widgets.ColMuted, 10)
	headMem := lbl("MEM", widgets.ColMuted, 10)
	headRow := container.New(procLayout, headPID, headCmd, headCPU, headMem)

	procObjs := []fyne.CanvasObject{headRow}
	for r := 0; r < numProcs; r++ {
		p.procPID[r] = lbl("--", widgets.ColMuted, 10)
		p.procName[r] = lbl("--", widgets.ColText, 11)
		p.procCPU[r] = blbl("--", widgets.ColOK, 11)
		p.procMem[r] = lbl("--", widgets.ColText, 11)
		row := container.New(procLayout,
			p.procPID[r], p.procName[r], p.procCPU[r], p.procMem[r])
		bg := canvas.NewRectangle(color.RGBA{0x1c, 0x22, 0x2a, 0xb0})
		bg.CornerRadius = 3
		procObjs = append(procObjs, container.NewStack(bg, container.NewPadded(row)))
	}
	procContent := container.New(rowsLayout{rows: numProcs + 1, gap: 2}, procObjs...)

	// ── Assemble ───────────────────────────────────────────
	cpuCard := widgets.Card("CPU", cpuContent)
	aiCard := widgets.Card("AI USAGE", aiContent)
	procCard := widgets.Card("TOP PROCESSES", procContent)

	top := container.New(equalGrid(2, 6), cpuCard, aiCard)
	return container.New(rowsLayout{rows: 2, gap: 6}, top, procCard)
}

// aiProvider wraps a header + body in a bordered sub-panel styled like the
// HTML .usage-provider cards (dark background, thin line border).
func aiProvider(header, body fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.RGBA{0x0a, 0x0e, 0x13, 0xff})
	bg.StrokeColor = widgets.ColLine
	bg.StrokeWidth = 1
	bg.CornerRadius = 7
	content := container.NewBorder(header, nil, nil, nil, body)
	return container.NewStack(bg, container.NewPadded(content))
}

func (p *corePage) Update(snap store.Snapshot) {
	// CPU gauge
	cpuFrac := snap.CPU.Total / 100.0
	cpuCol := pctColor(snap.CPU.Total)
	p.cpuRing.Set(cpuFrac, cpuCol)
	p.cpuPct.Text = fmt.Sprintf("%.0f%%", snap.CPU.Total)
	p.cpuPct.Color = cpuCol

	// Temp + freq chips
	p.tempChip.Text = fmt.Sprintf("TMP %.0f°", snap.Temp)
	p.tempChip.Color = tempColor(snap.Temp)
	if snap.CPUFreq > 0 {
		if snap.CPUFreq >= 1000 {
			p.freqChip.Text = fmt.Sprintf("CLK %.1fG", float64(snap.CPUFreq)/1000)
		} else {
			p.freqChip.Text = fmt.Sprintf("CLK %dM", snap.CPUFreq)
		}
	}

	// Core bars
	for i, v := range snap.CPU.Cores {
		if i >= numCores {
			break
		}
		col := pctColor(v)
		p.coreBar[i].Set(v/100.0, col)
		p.corePct[i].Text = fmt.Sprintf("%3.0f%%", v)
		p.corePct[i].Color = col
	}

	// RAM bar
	if snap.Memory.Total > 0 {
		ramFrac := snap.Memory.Used / snap.Memory.Total
		ramCol := pctColor(ramFrac * 100)
		p.ramBar.Set(ramFrac, ramCol)
		p.ramPct.Text = fmt.Sprintf("%.0f%%", ramFrac*100)
		p.ramPct.Color = ramCol
		p.ramDetail.Text = fmt.Sprintf("%.0f/%.0fM", snap.Memory.Used, snap.Memory.Total)
	}

	// DISK bar
	if snap.Disk.Total > 0 {
		diskFrac := snap.Disk.Used / snap.Disk.Total
		diskCol := pctColor(diskFrac * 100)
		p.diskBar.Set(diskFrac, diskCol)
		p.diskPct.Text = fmt.Sprintf("%.0f%%", diskFrac*100)
		p.diskPct.Color = diskCol
		p.diskDetail.Text = fmt.Sprintf("%.1f/%.1fG",
			snap.Disk.Used/1024, snap.Disk.Total/1024)
	}

	// OpenAI
	if snap.OpenAIPlan != nil {
		pl := snap.OpenAIPlan
		p.gptPill.Text = pl.PlanType
		p.gptPill.Color = usagePillColor(pl.PrimaryWindow.UsedPercent)

		p.gpt5hBar.Set(pl.PrimaryWindow.UsedPercent/100, barFillColor(pl.PrimaryWindow.UsedPercent, widgets.ColCyan))
		p.gpt5hPct.Text = fmt.Sprintf("%.0f%%", pl.PrimaryWindow.UsedPercent)
		p.gpt5hPct.Color = barFillColor(pl.PrimaryWindow.UsedPercent, widgets.ColCyan)
		p.gpt5hMeta.Text = resetInStr(pl.PrimaryWindow.ResetAfterSeconds)

		p.gpt1wBar.Set(pl.SecondaryWindow.UsedPercent/100, barFillColor(pl.SecondaryWindow.UsedPercent, color.RGBA{0x58, 0x80, 0xff, 0xff}))
		p.gpt1wPct.Text = fmt.Sprintf("%.0f%%", pl.SecondaryWindow.UsedPercent)
		p.gpt1wMeta.Text = resetInStr(pl.SecondaryWindow.ResetAfterSeconds)
	} else {
		p.gptPill.Text = "N/A"
		p.gptPill.Color = widgets.ColMuted
	}

	// Claude
	if snap.ClaudeUsage != nil {
		cu := snap.ClaudeUsage
		p.clPill.Text = cu.SubscriptionType
		p.clPill.Color = usagePillColor(claudePct(cu.FiveHourWindow))

		five := claudePct(cu.FiveHourWindow)
		p.cl5hBar.Set(five/100, barFillColor(five, widgets.ColViolet))
		p.cl5hPct.Text = pctText(cu.FiveHourWindow)
		p.cl5hPct.Color = barFillColor(five, widgets.ColViolet)
		p.cl5hMeta.Text = claudeReset(cu.FiveHourWindow)

		week := claudePct(cu.OneWeekWindow)
		p.cl1wBar.Set(week/100, barFillColor(week, widgets.ColCyan))
		p.cl1wPct.Text = pctText(cu.OneWeekWindow)
		p.cl1wMeta.Text = claudeReset(cu.OneWeekWindow)
	} else {
		p.clPill.Text = "N/A"
		p.clPill.Color = widgets.ColMuted
	}

	// Procs
	for r := 0; r < numProcs; r++ {
		if r < len(snap.Procs) {
			pr := snap.Procs[r]
			p.procPID[r].Text = pr.PID
			p.procName[r].Text = truncName(pr.Name, 16)
			p.procCPU[r].Text = fmt.Sprintf("%.1f%%", pr.CPU)
			p.procCPU[r].Color = pctColor(pr.CPU)
			p.procMem[r].Text = fmt.Sprintf("%.1f%%", pr.Mem)
		} else {
			p.procPID[r].Text = "--"
			p.procName[r].Text = "--"
			p.procCPU[r].Text = "--"
			p.procMem[r].Text = "--"
		}
	}

	p.cpuRing.Refresh()
	for i := range p.coreBar {
		p.coreBar[i].Refresh()
	}
	p.ramBar.Refresh()
	p.diskBar.Refresh()
	p.gpt5hBar.Refresh()
	p.gpt1wBar.Refresh()
	p.cl5hBar.Refresh()
	p.cl1wBar.Refresh()
	refreshAll(
		p.cpuPct, p.tempChip, p.freqChip,
		p.corePct[0], p.corePct[1], p.corePct[2], p.corePct[3],
		p.ramPct, p.ramDetail, p.diskPct, p.diskDetail,
		p.gptPill, p.gpt5hPct, p.gpt5hMeta, p.gpt1wPct, p.gpt1wMeta,
		p.clPill, p.cl5hPct, p.cl5hMeta, p.cl1wPct, p.cl1wMeta,
	)
	for r := 0; r < numProcs; r++ {
		refreshAll(p.procPID[r], p.procName[r], p.procCPU[r], p.procMem[r])
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func usagePillColor(pct float64) color.Color {
	switch {
	case pct >= 90:
		return widgets.ColViolet
	case pct >= 70:
		return widgets.ColAmber
	default:
		return widgets.ColOK
	}
}

func barFillColor(pct float64, dflt color.Color) color.Color {
	if pct >= 90 {
		return widgets.ColViolet
	}
	if pct >= 70 {
		return widgets.ColAmber
	}
	return dflt
}

func claudePct(w *claude.Window) float64 {
	if w == nil || w.UsedPercent == nil {
		return 0
	}
	return *w.UsedPercent
}

func claudeReset(w *claude.Window) string {
	if w == nil {
		return "--"
	}
	return resetInStr(w.ResetAfterSeconds)
}

func resetInStr(secs int64) string {
	if secs <= 0 {
		return "--"
	}
	d := time.Duration(secs) * time.Second
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("in %dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("in %dh", int(d.Hours()))
	default:
		return fmt.Sprintf("in %dm", int(d.Minutes()))
	}
}

func truncName(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
