// Package metrics samples host-level system metrics from /proc, /sys, and
// a few external commands. It mirrors the data shape that server.py was
// returning under /api/status.
package metrics

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type CPU struct {
	Total   float64   `json:"total"`
	MaxCore float64   `json:"max_core"`
	Cores   []float64 `json:"cores"`
}

type Mem struct {
	Total float64 `json:"total"`
	Used  float64 `json:"used"`
}

type Power struct {
	Available     bool   `json:"available"`
	ThrottledHex  string `json:"throttled_hex"`
	UnderVoltage  bool   `json:"under_voltage"`
	FreqCapped    bool   `json:"freq_capped"`
	Throttled     bool   `json:"throttled"`
	SoftTempLimit bool   `json:"soft_temp_limit"`
	Summary       string `json:"summary"`
}

type Proc struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
	PID  string  `json:"pid"`
	User string  `json:"user"`
}

// Sampler holds the state needed for rate-based metrics (CPU%, net B/s, disk B/s).
// Construct one and reuse it across polls.
type Sampler struct {
	cpuPrev  map[string]cpuStat
	netPrev  ratePrev
	diskPrev ratePrev
}

type cpuStat struct{ total, idle uint64 }
type ratePrev struct {
	rx, tx uint64
	ts     time.Time
}

func NewSampler() *Sampler {
	return &Sampler{cpuPrev: map[string]cpuStat{}}
}

func Temp() float64 {
	raw, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil {
		return 0
	}
	return v / 1000
}

func (s *Sampler) CPU() CPU {
	curr, err := readCPUStat()
	if err != nil {
		return CPU{}
	}
	if len(s.cpuPrev) == 0 {
		s.cpuPrev = curr
		return CPU{}
	}

	pct := func(name string) float64 {
		c, okC := curr[name]
		p, okP := s.cpuPrev[name]
		if !okC || !okP {
			return 0
		}
		dt := int64(c.total) - int64(p.total)
		di := int64(c.idle) - int64(p.idle)
		if dt <= 0 {
			return 0
		}
		return math.Max(0, 100.0*(1.0-float64(di)/float64(dt)))
	}

	total := round1(pct("cpu"))

	names := make([]string, 0, len(curr))
	for k := range curr {
		if k != "cpu" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	cores := make([]float64, len(names))
	maxCore := 0.0
	for i, n := range names {
		v := round1(pct(n))
		cores[i] = v
		if v > maxCore {
			maxCore = v
		}
	}
	if maxCore == 0 {
		maxCore = total
	}

	s.cpuPrev = curr
	return CPU{Total: total, MaxCore: maxCore, Cores: cores}
}

func readCPUStat() (map[string]cpuStat, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]cpuStat{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu") {
			break
		}
		parts := strings.Fields(line)
		if len(parts) < 5 {
			continue
		}
		vals := make([]uint64, 0, len(parts)-1)
		for _, s := range parts[1:] {
			n, _ := strconv.ParseUint(s, 10, 64)
			vals = append(vals, n)
		}
		idle := vals[3]
		if len(vals) > 4 {
			idle += vals[4]
		}
		var total uint64
		for _, v := range vals {
			total += v
		}
		out[parts[0]] = cpuStat{total: total, idle: idle}
	}
	return out, sc.Err()
}

// MemorySwap returns (mem, swap) in MiB to match the original `free -m` shape.
func MemorySwap() (Mem, Mem) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return Mem{}, Mem{}
	}
	defer f.Close()

	vals := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), ":")
		v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), " kB"))
		n, _ := strconv.ParseUint(v, 10, 64)
		vals[k] = n
	}
	toMiB := func(kB uint64) float64 { return float64(kB) / 1024.0 }

	memTotal := vals["MemTotal"]
	memAvail, ok := vals["MemAvailable"]
	if !ok {
		// Fall back to free+buffers+cached
		memAvail = vals["MemFree"] + vals["Buffers"] + vals["Cached"]
	}
	memUsed := uint64(0)
	if memTotal > memAvail {
		memUsed = memTotal - memAvail
	}

	swapTotal := vals["SwapTotal"]
	swapFree := vals["SwapFree"]
	swapUsed := uint64(0)
	if swapTotal > swapFree {
		swapUsed = swapTotal - swapFree
	}

	return Mem{Total: toMiB(memTotal), Used: toMiB(memUsed)},
		Mem{Total: toMiB(swapTotal), Used: toMiB(swapUsed)}
}

// Disk returns root filesystem usage in GiB.
func Disk() Mem {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return Mem{}
	}
	total := float64(st.Blocks*uint64(st.Bsize)) / (1 << 30)
	free := float64(st.Bavail*uint64(st.Bsize)) / (1 << 30)
	return Mem{Total: total, Used: total - free}
}

// CPUFreq returns CPU0 freq in MHz, falling back to vcgencmd.
func CPUFreq() int {
	if raw, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
			return n / 1000
		}
	}
	if out, err := runCmd(2*time.Second, "vcgencmd", "measure_clock", "arm"); err == nil {
		_, after, ok := strings.Cut(out, "=")
		if ok {
			if n, err := strconv.Atoi(strings.TrimSpace(after)); err == nil && n > 100_000_000 {
				return n / 1_000_000
			}
		}
	}
	return 0
}

func Uptime() float64 {
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(raw))
	if len(parts) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(parts[0], 64)
	return v
}

func Load() [3]float64 {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return [3]float64{}
	}
	parts := strings.Fields(string(raw))
	if len(parts) < 3 {
		return [3]float64{}
	}
	var out [3]float64
	for i := 0; i < 3; i++ {
		out[i], _ = strconv.ParseFloat(parts[i], 64)
	}
	return out
}

// Network returns (rx, tx) bytes/sec across eth*/wlan* interfaces.
func (s *Sampler) Network() (rx, tx float64) {
	curr, ts := readNetDev()
	if s.netPrev.ts.IsZero() {
		s.netPrev = ratePrev{rx: curr.rx, tx: curr.tx, ts: ts}
		return 0, 0
	}
	dt := ts.Sub(s.netPrev.ts).Seconds()
	if dt > 0 {
		rx = math.Max(0, float64(curr.rx-s.netPrev.rx)/dt)
		tx = math.Max(0, float64(curr.tx-s.netPrev.tx)/dt)
	}
	s.netPrev = ratePrev{rx: curr.rx, tx: curr.tx, ts: ts}
	return
}

func readNetDev() (ratePrev, time.Time) {
	f, err := os.Open("/proc/net/dev")
	now := time.Now()
	if err != nil {
		return ratePrev{}, now
	}
	defer f.Close()
	var rx, tx uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !(strings.Contains(line, "eth") || strings.Contains(line, "wlan")) {
			continue
		}
		_, after, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		parts := strings.Fields(after)
		if len(parts) < 9 {
			continue
		}
		rxN, _ := strconv.ParseUint(parts[0], 10, 64)
		txN, _ := strconv.ParseUint(parts[8], 10, 64)
		rx += rxN
		tx += txN
	}
	return ratePrev{rx: rx, tx: tx}, now
}

// DiskIO returns (read, write) bytes/sec across mmcblk0/sda.
func (s *Sampler) DiskIO() (rd, wt float64) {
	curr, ts := readDiskstats()
	if s.diskPrev.ts.IsZero() {
		s.diskPrev = ratePrev{rx: curr.rx, tx: curr.tx, ts: ts}
		return 0, 0
	}
	dt := ts.Sub(s.diskPrev.ts).Seconds()
	if dt > 0 {
		rd = math.Max(0, float64(curr.rx-s.diskPrev.rx)/dt)
		wt = math.Max(0, float64(curr.tx-s.diskPrev.tx)/dt)
	}
	s.diskPrev = ratePrev{rx: curr.rx, tx: curr.tx, ts: ts}
	return
}

func readDiskstats() (ratePrev, time.Time) {
	f, err := os.Open("/proc/diskstats")
	now := time.Now()
	if err != nil {
		return ratePrev{}, now
	}
	defer f.Close()
	var rd, wt uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		parts := strings.Fields(line)
		if len(parts) < 10 {
			continue
		}
		name := parts[2]
		if name != "mmcblk0" && name != "sda" {
			continue
		}
		// sectors read (5) and written (9), 512 bytes each
		r, _ := strconv.ParseUint(parts[5], 10, 64)
		w, _ := strconv.ParseUint(parts[9], 10, 64)
		rd += r * 512
		wt += w * 512
	}
	return ratePrev{rx: rd, tx: wt}, now
}

func PowerStatus() Power {
	p := Power{Summary: "unknown"}
	out, err := runCmd(2*time.Second, "vcgencmd", "get_throttled")
	if err != nil {
		return p
	}
	_, after, ok := strings.Cut(strings.TrimSpace(out), "=")
	if !ok {
		return p
	}
	hex := strings.TrimPrefix(strings.TrimSpace(after), "0x")
	v, err := strconv.ParseUint(hex, 16, 64)
	if err != nil {
		return p
	}
	p.Available = true
	p.ThrottledHex = fmt.Sprintf("0x%x", v)
	p.UnderVoltage = v&0x1 != 0 || v&0x10000 != 0
	p.FreqCapped = v&0x2 != 0 || v&0x20000 != 0
	p.Throttled = v&0x4 != 0 || v&0x40000 != 0
	p.SoftTempLimit = v&0x8 != 0 || v&0x80000 != 0
	var flags []string
	if p.UnderVoltage {
		flags = append(flags, "undervoltage")
	}
	if p.FreqCapped {
		flags = append(flags, "freq cap")
	}
	if p.Throttled {
		flags = append(flags, "throttled")
	}
	if p.SoftTempLimit {
		flags = append(flags, "soft temp")
	}
	if len(flags) == 0 {
		p.Summary = "nominal"
	} else {
		p.Summary = strings.Join(flags, ", ")
	}
	return p
}

// TopProcs returns up to n processes sorted by %CPU desc, excluding chromium.
func TopProcs(n int) []Proc {
	out, err := runCmd(5*time.Second, "ps", "aux", "--sort=-%cpu")
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) <= 1 {
		return nil
	}
	procs := make([]Proc, 0, n)
	for _, line := range lines[1:] {
		parts := splitN(line, 11)
		if len(parts) < 11 {
			continue
		}
		cmd := parts[10]
		if strings.Contains(strings.ToLower(cmd), "chromium") ||
			strings.Contains(strings.ToLower(cmd), "headless_shell") {
			continue
		}
		exeAndArgs := strings.SplitN(cmd, " ", 2)
		exe := exeAndArgs[0]
		if idx := strings.LastIndex(exe, "/"); idx >= 0 {
			exe = exe[idx+1:]
		}
		name := exe
		if len(exeAndArgs) > 1 {
			name = exe + " " + exeAndArgs[1]
		}
		if len(name) > 45 {
			name = name[:45]
		}
		cpu, _ := strconv.ParseFloat(parts[2], 64)
		mem, _ := strconv.ParseFloat(parts[3], 64)
		user := parts[0]
		if len(user) > 8 {
			user = user[:8]
		}
		procs = append(procs, Proc{Name: name, CPU: cpu, Mem: mem, PID: parts[1], User: user})
		if len(procs) >= n {
			break
		}
	}
	return procs
}

// splitN splits on runs of whitespace into at most n fields; the last field keeps the remainder verbatim.
func splitN(s string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n-1; i++ {
		s = strings.TrimLeft(s, " \t")
		end := strings.IndexAny(s, " \t")
		if end < 0 {
			out = append(out, s)
			return out
		}
		out = append(out, s[:end])
		s = s[end:]
	}
	out = append(out, strings.TrimLeft(s, " \t"))
	return out
}

func Hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return ""
}

func Kernel() string {
	out, err := runCmd(2*time.Second, "uname", "-r")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// PythonVersion is retained because the mission page shows it.
func PythonVersion() string {
	out, err := runCmd(2*time.Second, "python3", "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// ProcAndThreadCount returns total process and thread counts.
func ProcAndThreadCount() (procs, threads int) {
	out, err := runCmd(3*time.Second, "ps", "aux", "--no-headers")
	if err == nil {
		procs = strings.Count(strings.TrimSpace(out), "\n") + 1
	}
	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(raw))
		if len(parts) >= 4 {
			// "running/total"
			if _, after, ok := strings.Cut(parts[3], "/"); ok {
				threads, _ = strconv.Atoi(after)
			}
		}
	}
	return
}

func runCmd(d time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
