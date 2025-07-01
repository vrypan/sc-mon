package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
)

const (
	refreshInt = 2 * time.Second
	historyLen = 100
)

type heightSample struct {
	height int
	time   time.Time
}

type shardInfo struct {
	ShardId         int `json:"shardId"`
	MaxHeight       int `json:"maxHeight"`
	BlockDelay      int `json:"blockDelay"`
	History         []heightSample
	BlocksPerSec    float64
	AvgBlocksPerSec float64
	PrevRate        float64
}

type infoResponse struct {
	ShardInfos []shardInfo `json:"shardInfos"`
}

type model struct {
	shards       []shardInfo
	err          error
	apiURL       string
	maxRate      float64
	barWidth     int
	rateBarWidth int
}

type errMsg struct{ error }
type dataMsg struct {
	shards []shardInfo
	time   time.Time
}

var (
	meterStyleLight = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	redStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	yellowStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	greenStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	titleStyle      = lipgloss.NewStyle().Bold(true)
)

func Run(apiURL string) {
	p := tea.NewProgram(NewModel(apiURL))
	if _, err := p.Run(); err != nil {
		fmt.Println("Error starting app:", err)
		os.Exit(1)
	}
}

func NewModel(apiURL string) model {
	return model{
		apiURL:       apiURL,
		barWidth:     100,
		rateBarWidth: 100,
	}
}

func (m model) Init() tea.Cmd {
	return m.doTick()
}

func (m model) doTick() tea.Cmd {
	return tea.Tick(refreshInt, func(t time.Time) tea.Msg {
		return fetchData(m.apiURL)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.barWidth = msg.Width - 30 // adjust padding as needed
		if m.barWidth < 10 {
			m.barWidth = 10
		}

		m.rateBarWidth = msg.Width - 30 // adjust padding as needed
		if m.rateBarWidth < 10 {
			m.rateBarWidth = 10
		}
		return m, nil
	case errMsg:
		m.err = msg
		return m, m.doTick()
	case dataMsg:
		maxRate := 0.0
		shardPrevMap := make(map[int]shardInfo, len(m.shards))
		for _, prev := range m.shards {
			shardPrevMap[prev.ShardId] = prev
		}
		for i := range msg.shards {
			sh := &msg.shards[i]
			prev, ok := shardPrevMap[sh.ShardId]
			var prevHist []heightSample
			var prevRate float64
			if ok {
				prevHist = prev.History
				prevRate = prev.BlocksPerSec
			}
			sh.History = append(prevHist, heightSample{height: sh.MaxHeight, time: msg.time})
			if len(sh.History) > historyLen {
				sh.History = sh.History[len(sh.History)-historyLen:]
			}
			if len(sh.History) >= 2 {
				start := sh.History[0]
				end := sh.History[len(sh.History)-1]
				deltaH := float64(end.height - start.height)
				deltaT := end.time.Sub(start.time).Seconds()
				if deltaT > 0 {
					sh.BlocksPerSec = deltaH / deltaT
					if sh.BlocksPerSec > maxRate {
						maxRate = sh.BlocksPerSec
					}
				}

				// Calculate average block rate across history
				var totalBlocks, totalTime float64
				for j := 1; j < len(sh.History); j++ {
					dh := float64(sh.History[j].height - sh.History[j-1].height)
					dt := sh.History[j].time.Sub(sh.History[j-1].time).Seconds()
					if dt > 0 {
						totalBlocks += dh
						totalTime += dt
					}
				}
				if totalTime > 0 {
					sh.AvgBlocksPerSec = totalBlocks / totalTime
				}
			}
			sh.PrevRate = prevRate
			msg.shards[i] = *sh
		}
		m.shards = msg.shards
		m.maxRate = maxRate
		return m, m.doTick()
	}
	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		errMsg := fmt.Sprintf("Error: %v", m.err)
		return wrapText(errMsg, 80)
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Snapchain Node Monitor") + " ")
	sb.WriteString(fmt.Sprintf("%s\n\n", time.Now().Format(time.RFC1123)))

	for _, shard := range m.shards {
		total := float64(shard.MaxHeight + shard.BlockDelay)
		ratio := float64(shard.MaxHeight) / math.Max(1, total)
		syncPct := fmt.Sprintf("%.1f%%", ratio*100)
		syncBar := fmt.Sprintf("%s %s", m.syncBarColored(ratio), syncPct)

		eta := "∞"
		if shard.BlocksPerSec > 0 {
			etaSec := int(float64(shard.BlockDelay) / shard.AvgBlocksPerSec)
			eta = formatTime(etaSec)
		}

		maxRate := math.Max(0.0001, m.maxRate)
		rateRatio := shard.BlocksPerSec / maxRate
		rateRatio = math.Min(rateRatio, 1.0)
		meterFilled := int(rateRatio * float64(m.rateBarWidth))
		meter := fmt.Sprintf("[%s%s]",
			meterStyleLight.Render(strings.Repeat("■", meterFilled)),
			meterStyleLight.Render(strings.Repeat(" ", m.rateBarWidth-meterFilled)),
		)

		arrow := trendArrow(shard.BlocksPerSec, shard.PrevRate)

		sb.WriteString(fmt.Sprintf(
			"Shard %d | Height: %-10s | Delay: %-10s | ETA: %s\nSync status: %s\nBlocks/sec:  %s %.2f blk/s %s\n\n",
			shard.ShardId,
			humanize.Comma(int64(shard.MaxHeight)),
			humanize.Comma(int64(shard.BlockDelay)),
			eta,
			syncBar,
			meter,
			shard.BlocksPerSec,
			arrow,
		))
	}

	sb.WriteString("(Press 'q' or ESC to quit)")
	return sb.String()
}

func (m model) syncBarColored(ratio float64) string {
	filled := int(ratio * float64(m.barWidth))
	empty := m.barWidth - filled

	style := redStyle
	if ratio > 0.7 {
		style = greenStyle
	} else if ratio > 0.4 {
		style = yellowStyle
	}

	return fmt.Sprintf("[%s%s]",
		style.Render(strings.Repeat("■", filled)),
		style.Render(strings.Repeat(" ", empty)))
}

func trendArrow(current, prev float64) string {
	switch {
	case current > prev:
		return "↗"
	case current < prev:
		return "↘"
	default:
		return "→"
	}
}

func fetchData(apiURL string) tea.Msg {
	resp, err := http.Get(apiURL)
	if err != nil {
		return errMsg{err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errMsg{err}
	}

	var result infoResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return errMsg{fmt.Errorf("json=%s, err=%v", body, err)}
	}

	return dataMsg{result.ShardInfos, time.Now()}
}

func formatTime(seconds int) string {
	d := time.Duration(seconds) * time.Second
	if d.Hours() >= 24 {
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		return fmt.Sprintf("%dd %dh", days, hours)
	} else if d.Hours() >= 1 {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", hours, minutes)
	} else {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}

	var result string
	for i := 0; i < len(s); i += width {
		end := i + width
		if end > len(s) {
			end = len(s)
		}
		result += s[i:end] + "\n"
	}
	return result
}
