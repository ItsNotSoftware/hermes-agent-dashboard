// Package store owns the application's polling loops and the in-memory
// snapshot that the UI reads from. Each data source runs at the cadence
// that matches its real freshness and cost; the UI subscribes to a
// notification channel and re-renders on update.
package store

import (
	"context"
	"sync"
	"time"

	"github.com/diogo/hermes-agent-dashboard/internal/calendar"
	"github.com/diogo/hermes-agent-dashboard/internal/claude"
	"github.com/diogo/hermes-agent-dashboard/internal/derive"
	"github.com/diogo/hermes-agent-dashboard/internal/health"
	"github.com/diogo/hermes-agent-dashboard/internal/hermes"
	"github.com/diogo/hermes-agent-dashboard/internal/loops"
	"github.com/diogo/hermes-agent-dashboard/internal/metrics"
	"github.com/diogo/hermes-agent-dashboard/internal/openai"
	"github.com/diogo/hermes-agent-dashboard/internal/repo"
	"github.com/diogo/hermes-agent-dashboard/internal/vault"
)

type Snapshot struct {
	// System (fast tier)
	ModelInfo hermes.ModelInfo `json:"model_info"`
	Temp      float64          `json:"temp"`
	CPU       metrics.CPU      `json:"cpu"`
	Memory    metrics.Mem      `json:"memory"`
	Swap      metrics.Mem      `json:"swap"`
	Disk      metrics.Mem      `json:"disk"`
	CPUFreq   int              `json:"cpu_freq"`
	Uptime    float64          `json:"uptime"`
	NetRx     float64          `json:"net_rx"`
	NetTx     float64          `json:"net_tx"`
	DiskRd    float64          `json:"disk_rd"`
	DiskWt    float64          `json:"disk_wt"`
	Load      [3]float64       `json:"load"`
	Power     metrics.Power    `json:"power"`
	Procs     []metrics.Proc   `json:"procs"`
	Hostname  string           `json:"hostname"`
	Kernel    string           `json:"kernel"`
	Python    string           `json:"python"`
	ProcCount int              `json:"proc_count"`
	Threads   int              `json:"threads"`

	// Crons + derived
	Crons          []hermes.Cron              `json:"crons"`
	AgentOps       map[string]derive.AgentOps `json:"agent_ops"`
	MissionLog     []derive.MissionEvent      `json:"mission_log"`
	MissionControl derive.MissionControl      `json:"mission_control"`

	// External services
	ServiceHealth health.Data   `json:"service_health"`
	OpenAIPlan    *openai.Plan  `json:"openai_plan"`
	ClaudeUsage   *claude.Usage `json:"claude_usage"`
	VaultIntel    vault.Intel   `json:"vault_intel"`
	Loops         loops.Data    `json:"obsidian_loops"`
	Repo          repo.Status   `json:"repo_status"`
	Calendar      calendar.Data `json:"calendar_upcoming"`

	UpdatedAt time.Time `json:"updated_at"`
}

type Store struct {
	mu      sync.RWMutex
	snap    Snapshot
	sampler *metrics.Sampler

	dashboardDir string
	port         int

	subsMu sync.Mutex
	subs   []chan struct{}
}

func New(dashboardDir string, port int) *Store {
	return &Store{
		sampler:      metrics.NewSampler(),
		dashboardDir: dashboardDir,
		port:         port,
	}
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

// Subscribe returns a channel that receives one signal per update. Drops
// signals if the subscriber isn't reading; the UI just needs to refresh.
func (s *Store) Subscribe() <-chan struct{} {
	ch := make(chan struct{}, 1)
	s.subsMu.Lock()
	s.subs = append(s.subs, ch)
	s.subsMu.Unlock()
	return ch
}

func (s *Store) notify() {
	s.subsMu.Lock()
	subs := append([]chan struct{}{}, s.subs...)
	s.subsMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *Store) update(fn func(snap *Snapshot)) {
	s.mu.Lock()
	fn(&s.snap)
	s.snap.UpdatedAt = time.Now()
	s.recomputeDerivedLocked()
	s.mu.Unlock()
	s.notify()
}

func (s *Store) recomputeDerivedLocked() {
	s.snap.AgentOps = derive.Ops(hermes.DefaultCronSources(), s.snap.Crons)
	s.snap.MissionLog = derive.Log(s.snap.Crons, s.snap.OpenAIPlan, s.snap.ClaudeUsage, s.snap.Memory, s.snap.Disk, s.snap.Temp)
	s.snap.MissionControl = derive.Control(s.snap.Calendar, s.snap.Loops, s.snap.Crons)
}

// Start launches the polling goroutines. They stop when ctx is cancelled.
func (s *Store) Start(ctx context.Context) {
	s.refreshFast()
	s.refreshCrons()
	go s.tick(ctx, 1*time.Second, s.refreshFast)
	go s.tick(ctx, 5*time.Second, s.refreshCrons)
	go s.tick(ctx, 10*time.Second, s.refreshRepo)
	go s.tick(ctx, 30*time.Second, s.refreshHealth)
	go s.tick(ctx, 30*time.Second, s.refreshOpenAI)
	go s.tick(ctx, 60*time.Second, s.refreshLoops)
	go s.tick(ctx, 60*time.Second, s.refreshVault)
	go s.tick(ctx, 120*time.Second, s.refreshClaude)
	go s.tick(ctx, 120*time.Second, s.refreshCalendar)
	go s.tick(ctx, 30*time.Second, s.refreshModelInfo)
}

func (s *Store) tick(ctx context.Context, d time.Duration, fn func()) {
	t := time.NewTicker(d)
	defer t.Stop()
	// Run once immediately so subscribers don't wait a full interval.
	fn()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

// --- pollers ---

func (s *Store) refreshFast() {
	cpu := s.sampler.CPU()
	mem, swap := metrics.MemorySwap()
	disk := metrics.Disk()
	rx, tx := s.sampler.Network()
	rd, wt := s.sampler.DiskIO()
	temp := metrics.Temp()
	load := metrics.Load()
	freq := metrics.CPUFreq()
	power := metrics.PowerStatus()
	procs := metrics.TopProcs(7)
	uptime := metrics.Uptime()
	host := metrics.Hostname()
	kernel := metrics.Kernel()
	pyver := metrics.PythonVersion()
	procCount, threads := metrics.ProcAndThreadCount()

	s.update(func(sn *Snapshot) {
		sn.CPU = cpu
		sn.Memory = mem
		sn.Swap = swap
		sn.Disk = disk
		sn.NetRx = rx
		sn.NetTx = tx
		sn.DiskRd = rd
		sn.DiskWt = wt
		sn.Temp = temp
		sn.Load = load
		sn.CPUFreq = freq
		sn.Power = power
		sn.Procs = procs
		sn.Uptime = uptime
		sn.Hostname = host
		sn.Kernel = kernel
		sn.Python = pyver
		sn.ProcCount = procCount
		sn.Threads = threads
	})
}

func (s *Store) refreshCrons() {
	jobs := hermes.CronJobs(hermes.DefaultCronSources())
	s.update(func(sn *Snapshot) { sn.Crons = jobs })
}

func (s *Store) refreshRepo() {
	st := repo.Read(context.Background(), s.dashboardDir)
	s.update(func(sn *Snapshot) { sn.Repo = st })
}

func (s *Store) refreshHealth() {
	d := health.Collect(context.Background(), s.port)
	s.update(func(sn *Snapshot) { sn.ServiceHealth = d })
}

func (s *Store) refreshOpenAI() {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	plan, err := openai.Fetch(ctx)
	if err != nil {
		// Keep the previous plan visible during transient failures.
		return
	}
	s.update(func(sn *Snapshot) { sn.OpenAIPlan = plan })
}

func (s *Store) refreshClaude() {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	u, err := claude.Fetch(ctx, s.dashboardDir)
	if err != nil {
		return
	}
	s.update(func(sn *Snapshot) { sn.ClaudeUsage = u })
}

func (s *Store) refreshLoops() {
	d := loops.Collect()
	s.update(func(sn *Snapshot) { sn.Loops = d })
}

func (s *Store) refreshVault() {
	v := vault.Scan()
	s.update(func(sn *Snapshot) { sn.VaultIntel = v })
}

func (s *Store) refreshCalendar() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := calendar.Upcoming(ctx)
	s.update(func(sn *Snapshot) { sn.Calendar = c })
}

func (s *Store) refreshModelInfo() {
	info := hermes.ModelConfig()
	s.update(func(sn *Snapshot) { sn.ModelInfo = info })
}
