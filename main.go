// Ceartax v2.3 - BENCHMARKED + BUBBLE TEA HYPER-OPTIMIZED
// Kredit: Mr. Front-X
// FITUR: Live benchmark, TUI 60 FPS, zero-jank, memory profiling, HTML graph
// Compile: go build -o ceartax main.go -ldflags="-s -w"
// Usage: ./ceartax -url target.com -ua-file ua.txt

package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/net/proxy"
	"golang.org/x/sync/errgroup"
)

// === BENCHMARK STRUCT ===
type Benchmark struct {
	Module     string        `json:"module"`
	Start      time.Time     `json:"start"`
	End        time.Time     `json:"end"`
	Duration   time.Duration `json:"duration_ms"`
	Requests   int           `json:"requests"`
	RPS        float64       `json:"rps"`
	MemoryPre  uint64        `json:"mem_pre_kb"`
	MemoryPost uint64        `json:"mem_post_kb"`
	DeltaKB    int64         `json:"mem_delta_kb"`
	Status     string        `json:"status"`
}

// === STYLING (PRE-CACHED) ===
var (
	titleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true).Align(lipgloss.Center)
	infoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF41"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00"))
	barStyle     = lipgloss.NewStyle().Width(30)
	fpsStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF00FF"))
)

// === TUI MESSAGES ===
type frameMsg struct{}
type progressMsg struct{ module string; value float64 }
type benchMsg struct{ b Benchmark }
type doneMsg struct{}

// === TUI MODEL ===
type model struct {
	ceartax      *Ceartax
	progress     map[string]progress.Model
	spinner      spinner.Model
	width        int
	phase        string
	benchmarks   []Benchmark
	startTime    time.Time
	frameCount   int
	lastFrame    time.Time
	fps          float64
	repaintCh    chan struct{}
	ready        bool
}

func initialModel(c *Ceartax) model {
	return model{
		ceartax:    c,
		progress:   make(map[string]progress.Model),
		spinner:    spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		phase:      "Initializing...",
		startTime:  time.Now(),
		lastFrame:  time.Now(),
		repaintCh:  make(chan struct{}, 1),
	}
}

// === CEARTAX CORE ===
type Ceartax struct {
	target   string
	proxyURL string
	timeout  time.Duration
	output   string
	uaList   []string
	client   *http.Client
	result   ReconResult
	mu       sync.Mutex
	chProg   chan progressMsg
	chBench  chan benchMsg
	chDone   chan doneMsg
	pool     *errgroup.Group
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewCeartax(target, proxyURL, uaFile, output string, timeout time.Duration) *Ceartax {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Ceartax{
		target:   target,
		proxyURL: proxyURL,
		timeout:  timeout,
		output:   output,
		result: ReconResult{
			Target:    target,
			TechStack: make(map[string]string),
			Headers:   make(map[string]string),
			TLSInfo:   make(map[string]string),
			Timestamp: time.Now(),
		},
		chProg:  make(chan progressMsg, 50),
		chBench: make(chan benchMsg, 10),
		chDone:  make(chan doneMsg, 1),
		pool:    &errgroup.Group{},
		ctx:     ctx,
		cancel:  cancel,
	}
	c.loadUAs(uaFile)
	c.initClient()
	return c
}

func (c *Ceartax) loadUAs(file string) {
	f, _ := os.Open(file)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if ua := strings.TrimSpace(sc.Text()); ua != "" && !strings.HasPrefix(ua, "#") {
			c.uaList = append(c.uaList, ua)
		}
	}
	f.Close()
	if len(c.uaList) == 0 {
		c.uaList = []string{"Ceartax/2.3"}
	}
}

func (c *Ceartax) randomUA() string {
	return c.uaList[rand.Intn(len(c.uaList))]
}

func (c *Ceartax) initClient() {
	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:      30,
		IdleConnTimeout:   20 * time.Second,
		DisableKeepAlives: false,
	}
	if c.proxyURL != "" {
		dialer, _ := proxy.SOCKS5("tcp", strings.TrimPrefix(c.proxyURL, "socks5://"), nil, proxy.Direct)
		tr.DialContext = dialer.DialContext
	}
	c.client = &http.Client{Transport: tr, Timeout: c.timeout}
}

func (c *Ceartax) randomDelay() {
	select {
	case <-c.ctx.Done():
	case <-time.After(time.Duration(rand.Intn(2)+1) * time.Second):
	}
}

func (c *Ceartax) runBench(name string, fn func()) {
	c.pool.Go(func() error {
		b := Benchmark{
			Module:    name,
			Start:     time.Now(),
			MemoryPre: c.memKB(),
		}
		defer func() {
			b.End = time.Now()
			b.Duration = b.End.Sub(b.Start)
			b.MemoryPost = c.memKB()
			b.DeltaKB = int64(b.MemoryPost) - int64(b.MemoryPre)
			b.Status = "DONE"
			if b.Requests > 0 {
				b.RPS = float64(b.Requests) / b.Duration.Seconds()
			}
			c.chBench <- benchMsg{b: b}
		}()
		fn()
		return nil
	})
}

func (c *Ceartax) memKB() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc / 1024
}

// === MODULES ===
func (c *Ceartax) Subdomains() {
	defer c.moduleDone()
	words := [...]string{"www", "api", "admin", "mail", "dev"}
	total := float64(len(words))
	for i, w := range words {
		c.randomDelay()
		if _, err := net.LookupHost(w + "." + c.target); err == nil {
			c.mu.Lock()
			c.result.Subdomains = append(c.result.Subdomains, w+"."+c.target)
			c.mu.Unlock()
		}
		c.chProg <- progressMsg{module: "sub", value: float64(i+1) / total}
	}
}

func (c *Ceartax) Ports() {
	defer c.moduleDone()
	ports := [...]int{80, 443, 22}
	total := float64(len(ports))
	for i, p := range ports {
		c.randomDelay()
		if conn, _ := net.DialTimeout("tcp", c.target+":"+fmt.Sprint(p), 1*time.Second); conn != nil {
			c.mu.Lock()
			c.result.OpenPorts = append(c.result.OpenPorts, p)
			c.mu.Unlock()
			conn.Close()
		}
		c.chProg <- progressMsg{module: "ports", value: float64(i+1) / total}
	}
}

func (c *Ceartax) Fingerprint() {
	defer c.moduleDone()
	req, _ := http.NewRequestWithContext(c.ctx, "GET", "https://"+c.target, nil)
	req.Header.Set("User-Agent", c.randomUA())
	if resp, err := c.client.Do(req); err == nil {
		defer resp.Body.Close()
		for k, v := range resp.Header {
			c.result.Headers[strings.ToLower(k)] = strings.Join(v, ", ")
		}
		c.chProg <- progressMsg{module: "fp", value: 1.0}
	}
}

func (c *Ceartax) Dirs() {
	defer c.moduleDone()
	dirs := [...]string{".git", "robots.txt", "admin"}
	ch := make(chan string, len(dirs))
	for _, d := range dirs { ch <- d }
	close(ch)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done()
			for d := range ch {
				u := "https://" + c.target + "/" + d
				req, _ := http.NewRequestWithContext(c.ctx, "HEAD", u, nil)
				req.Header.Set("User-Agent", c.randomUA())
				if resp, _ := c.client.Do(req); resp != nil && resp.StatusCode < 400 {
					c.mu.Lock()
					c.result.Directories = append(c.result.Directories, u)
					c.mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	c.chProg <- progressMsg{module: "dirs", value: 1.0}
}

func (c *Ceartax) moduleDone() { c.chDone <- doneMsg{} }

func (c *Ceartax) Run() {
	c.runBench("Subdomains", c.Subdomains)
	c.runBench("Ports", c.Ports)
	c.runBench("Fingerprint", c.Fingerprint)
	c.runBench("Directories", c.Dirs)
	go func() {
		c.pool.Wait()
		c.chDone <- doneMsg{}
	}()
}

// === TUI ===
func (m model) Init() tea.Cmd {
	m.ceartax.Run()
	return tea.Batch(
		m.frameCmd(),
		m.progressCmd(),
		m.benchCmd(),
	)
}

func (m model) frameCmd() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(t time.Time) tea.Msg {
		m.frameCount++
		if time.Since(m.lastFrame) >= time.Second {
			m.fps = float64(m.frameCount) / time.Since(m.lastFrame).Seconds()
			m.frameCount = 0
			m.lastFrame = time.Now()
		}
		select {
		case m.repaintCh <- struct{}{}:
		default:
		}
		return frameMsg{}
	})
}

func (m model) progressCmd() tea.Cmd {
	return func() tea.Msg {
		for p := range m.ceartax.chProg {
			return p
		}
		return nil
	}
}

func (m model) benchCmd() tea.Cmd {
	return func() tea.Msg {
		for b := range m.ceartax.chBench {
			return b
		}
		return nil
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg:
		if msg.(tea.KeyMsg).String() == "ctrl+c" {
			m.ceartax.cancel()
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.(tea.WindowSizeMsg).Width
	case progressMsg:
		p := msg.(progressMsg)
		if prog, ok := m.progress[p.module]; ok {
			prog.SetPercent(p.value)
		} else {
			prog = progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage())
			prog.SetPercent(p.value)
			m.progress[p.module] = prog
		}
	case benchMsg:
		m.benchmarks = append(m.benchmarks, msg.(benchMsg).b)
	case doneMsg:
		if len(m.benchmarks) >= 4 {
			m.ready = true
			m.saveResults()
			return m, tea.Quit
		}
	case frameMsg:
		select {
		case <-m.repaintCh:
		default:
		}
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if !m.ready {
		s := titleStyle.Width(m.width).Render(" CEARTAX v2.3 ") + "\n"
		s += fmt.Sprintf("%s %s | FPS: %.1f\n\n", m.spinner.View(), m.phase, m.fps)

		order := []string{"sub", "ports", "fp", "dirs"}
		for _, k := range order {
			if p, ok := m.progress[k]; ok {
				label := map[string]string{"sub": "Subdomains", "ports": "Ports", "fp": "Fingerprint", "dirs": "Dirs"}[k]
				s += barStyle.Render(fmt.Sprintf(" %s: %s\n", label, p.View()))
			}
		}
		return s
	}

	dur := time.Since(m.startTime)
	s := successStyle.Render("RECON + BENCHMARK SELESAI\n\n")
	s += fmt.Sprintf("Duration: %s | FPS Avg: %.1f\n", dur.Round(time.Millisecond), m.fps)
	s += fmt.Sprintf("Memory: %d KB peak\n", runtime.MemStats{}.Alloc/1024)
	s += fmt.Sprintf("Output: %s\n", m.ceartax.output)
	return s
}

func (m *model) saveResults() {
	// JSON
	data, _ := json.MarshalIndent(m.ceartax.result, "", "  ")
	os.WriteFile(m.ceartax.output, data, 0644)

	// HTML with benchmark graph
	tmpl := template.Must(template.New("report").Parse(htmlReportTemplate))
	f, _ := os.Create(strings.Replace(m.ceartax.output, ".json", ".html", 1))
	type Data struct {
		Result ReconResult
		Bench  []Benchmark
	}
	tmpl.Execute(f, Data{Result: m.ceartax.result, Bench: m.benchmarks})
	f.Close()
}

const htmlReportTemplate = `<!DOCTYPE html><html><head><title>Ceartax Report</title>
<style>body{font:14px monospace;background:#000;color:#0f0;padding:20px;}
table,th,td{border:1px solid #0f0;border-collapse:collapse;padding:8px;}
canvas{border:1px solid #0f0;}</style>
<script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
</head><body>
<h1>Ceartax v2.3 Report</h1>
<p><b>Target:</b> {{.Result.Target}} | <b>Time:</b> {{.Result.Timestamp}}</p>

<h2>Performance Benchmark</h2>
<canvas id="benchChart" width="800" height="400"></canvas>
<script>
const ctx = document.getElementById('benchChart').getContext('2d');
new Chart(ctx, {
  type: 'bar',
  data: {
    labels: [{{range .Bench}}"{{.Module}}",{{end}}],
    datasets: [{
      label: 'Duration (ms)',
      data: [{{range .Bench}}{{.Duration.Milliseconds}},{{end}}],
      backgroundColor: '#0f0'
    }, {
      label: 'RPS',
      data: [{{range .Bench}}{{printf "%.2f" .RPS}},{{end}}],
      backgroundColor: '#0ff',
      yAxisID: 'y1'
    }]
  },
  options: { scales: { y1: { position: 'right' } } }
});
</script>

<h2>Findings</h2>
<ul>{{range .Result.Subdomains}}<li>{{.}}</li>{{end}}</ul>
</body></html>`

// === MAIN ===
func main() {
	target := flag.String("url", "", "Target")
	output := flag.String("output", "recon.json", "Output")
	proxyStr := flag.String("proxy", "", "Proxy")
	uaFile := flag.String("ua-file", "", "UA file")
	timeout := flag.Duration("timeout", 10*time.Second, "Timeout")
	flag.Parse()

	if *target == "" || *uaFile == "" {
		log.Fatal("Gunakan: -url target.com -ua-file ua.txt")
	}

	u, _ := url.Parse(*target)
	clean := strings.TrimSuffix(u.Hostname(), ".")

	ceartax := NewCeartax(clean, *proxyStr, *uaFile, *output, *timeout)

	p := tea.NewProgram(initialModel(ceartax), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT)
	<-c
}
