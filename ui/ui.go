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
	refreshInt   = 2 * time.Second
	barWidth     = 100
	historyLen   = 100
	rateBarWidth = barWidth
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
	shards  []shardInfo
	err     error
	apiURL  string
	maxRate float64
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
	if err := p.Start(); err != nil {
		fmt.Println("Error starting app:", err)
		os.Exit(1)
	}
}

func NewModel(apiURL string) model {
	return model{apiURL: apiURL}
}

func (m model) Init() tea.Cmd {
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
	case errMsg:
		m.err = msg
		return m, m.Init()
	case dataMsg:
		maxRate := 0.0
		for i := range msg.shards {
			sh := &msg.shards[i]
			prevHist := []heightSample{}
			prevRate := 0.0
			for _, prev := range m.shards {
				if prev.ShardId == sh.ShardId {
					prevHist = prev.History
					prevRate = prev.BlocksPerSec
					break
				}
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
				var totalBlocks float64
				var totalTime float64
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
		return m, m.Init()
	}
	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	var out string
	out += titleStyle.Render("Snapchain Node Monitor") + " "
	out += fmt.Sprintf("%s\n\n", time.Now().Format(time.RFC1123))

	for _, shard := range m.shards {
		total := float64(shard.MaxHeight + shard.BlockDelay)
		ratio := float64(shard.MaxHeight) / math.Max(1, total)
		syncPct := fmt.Sprintf("%.1f%%", ratio*100)
		syncBar := fmt.Sprintf("%s %s", syncBarColored(ratio), syncPct)
		//syncBar := syncBarColored(ratio)

		eta := "∞"
		if shard.BlocksPerSec > 0 {
			etaSec := int(float64(shard.BlockDelay) / shard.AvgBlocksPerSec)
			eta = formatTime(etaSec)
		}

		rateRatio := shard.BlocksPerSec / math.Max(0.0001, m.maxRate)
		if rateRatio > 1.0 {
			rateRatio = 1.0
		}
		meterFilled := int(rateRatio * float64(rateBarWidth))
		meter := fmt.Sprintf("[%s%s]",
			meterStyleLight.Render(strings.Repeat("■", meterFilled)),
			meterStyleLight.Render(strings.Repeat(" ", rateBarWidth-meterFilled)))

		arrow := trendArrow(shard.BlocksPerSec, shard.PrevRate)

		out += fmt.Sprintf(
			"Shard %d | Height: %-10s | Delay: %-10s | ETA: %s\nSync status: %s\nBlocks/sec:  %s %.2f blk/s %s\n\n",
			shard.ShardId,
			humanize.Comma(int64(shard.MaxHeight)),
			humanize.Comma(int64(shard.BlockDelay)),
			eta,
			syncBar,
			meter,
			shard.BlocksPerSec,
			arrow,
		)
	}

	out += "(Press 'q' or ESC to quit)"
	return out
}

func syncBarColored(ratio float64) string {
	filled := int(ratio * float64(barWidth))
	empty := barWidth - filled

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
	if current > prev {
		return "↗"
	} else if current < prev {
		return "↘"
	}
	return "→"
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
		return errMsg{err}
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
