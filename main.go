package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

const (
	padding    = 2
	maxWidth   = 80
	tapsLength = 4
	maxBpm     = 240
)

type model struct {
	bpm                 int
	timeSignatureTop    int
	timeSignatureBottom int
	beat                int
	progress            progress.Model
	screen              screen
	help                help.Model
	ticker              *time.Ticker
	selected            element
	metronomeOn         bool
	//beepBuffer              *beep.Buffer
	// running average of bpm based on taps
	tapsAverage [tapsLength]time.Duration
	lastTap     time.Time
}

var beepBuffer beep.StreamSeekCloser

func main() {
	prog := progress.New(progress.WithDefaultScaledGradient(), progress.WithoutPercentage())

	initialModel := model{
		bpm:                 60,
		timeSignatureTop:    4,
		timeSignatureBottom: 4,
		beat:                0,
		progress:            prog,
		screen:              mainScreen,
		help:                help.New(),
		ticker:              time.NewTicker(time.Minute / 60),
		metronomeOn:         true,
	}

	if err := tea.NewProgram(initialModel).Start(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

type tickMsg time.Time
type metronomeTickMsg time.Time
type screenChangeMsg screen

type mainKeyMapType map[string]key.Binding
type settingsKeyMapType map[string]key.Binding

func (k settingsKeyMapType) ShortHelp() []key.Binding {
	return []key.Binding{
		k["Escape"], k["Help"],
	}
}

func (k settingsKeyMapType) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k["Space"], k["Escape"]},
		{k["Tap"], k["Tab"]},
		{k["Up"], k["Down"]},
		{k["Help"]},
	}
}

var settingsKeyMap = settingsKeyMapType{
	"Help": key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "Toggle Help"),
	),
	"Space": key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("Space", "Start/Stop"),
	),
	"Up": key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑", "Increment Value"),
	),
	"Down": key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓", "Decrement Value"),
	),
	"Tab": key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("Tab", "Move Selection"),
	),
	"Escape": key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("Esc", "Exit/Back"),
	),
	"Tap": key.NewBinding(
		key.WithKeys("t", "T"),
		key.WithHelp("T", "Tap Speed"),
	),
}

func (k mainKeyMapType) ShortHelp() []key.Binding {
	return []key.Binding{
		k["Escape"], k["Help"],
	}
}

func (k mainKeyMapType) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k["Space"], k["Escape"]},
		{k["Tap"], k["Edit"]},
		{k["Help"]},
	}
}

var mainKeyMap = mainKeyMapType{
	"Help": key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "Toggle Help"),
	),
	"Space": key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("Space", "Start/Stop"),
	),
	"Escape": key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("Esc", "Exit/Back"),
	),
	"Edit": key.NewBinding(
		key.WithKeys("s", "S"),
		key.WithHelp("S", "Configure"),
	),
	"Tap": key.NewBinding(
		key.WithKeys("t", "T"),
		key.WithHelp("T", "Tap Speed"),
	),
}

type screen int

const (
	mainScreen screen = iota
	settingsScreen
)

type element int

const (
	noneElement element = iota
	timeSignatureTopElement
	timeSignatureBottomElement
	bpmElement
	numberOfElement
)

//go:embed wood-block.wav
var woodblock []byte

func (m model) Init() tea.Cmd {

	tickReader := bytes.NewReader(woodblock)

	var format beep.Format
	var err error

	if beepBuffer, format, err = wav.Decode(tickReader); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/50)); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return tea.Batch(tickCmd, m.metronomeTickerCmd)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		k := mainKeyMap
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
		switch {
		case key.Matches(msg, k["Quit"]):
			return m, tea.Quit

		case key.Matches(msg, k["Escape"]):
			switch m.screen {
			case settingsScreen:
				return m, screenChangeCmd(mainScreen)
			case mainScreen:
				return m, tea.Quit
			}

		case key.Matches(msg, k["Edit"]):
			switch m.screen {
			case mainScreen:
				return m, screenChangeCmd(settingsScreen)
			}
			return m, nil

		case key.Matches(msg, k["Help"]):
			m.help.ShowAll = !m.help.ShowAll

		case key.Matches(msg, k["Space"]):
			if m.screen != settingsScreen {
				if m.metronomeOn {
					return m, m.stopTickerCmd
				} else {
					return m, m.updateTickerCmd
				}
			}

		case key.Matches(msg, k["Tap"]):
			var avg time.Duration
			for i, v := range m.tapsAverage {
				if len(m.tapsAverage)-1 == i {
					m.tapsAverage[i] = time.Since(m.lastTap)
				} else {
					m.tapsAverage[i] = m.tapsAverage[i+1]
				}
				avg += v
			}
			m.lastTap = time.Now()
			a := avg / tapsLength
			if a != 0 && int(time.Minute/a) > 0 && int(time.Minute/a) <= maxBpm+1 {
				m.bpm = int(time.Minute / a)
				return m, m.updateTickerCmd
			} else if a != 0 && int(time.Minute/a) > maxBpm {
				m.bpm = maxBpm
				return m, m.updateTickerCmd
			}
		}
		if m.screen == settingsScreen {
			k := settingsKeyMap
			switch {

			case key.Matches(msg, k["Tab"]):
				m.selected = ((m.selected) % (numberOfElement - 1)) + 1

			case key.Matches(msg, k["Up"]):
				switch m.selected {
				case bpmElement:
					if m.bpm != maxBpm {
						m.bpm += 1
					}
				case timeSignatureTopElement:
					if m.timeSignatureTop != 16 {
						m.timeSignatureTop += 1
					}
				case timeSignatureBottomElement:
					if m.timeSignatureBottom != 16 {
						m.timeSignatureBottom += 1
					}
				}

			case key.Matches(msg, k["Down"]):
				switch m.selected {
				case bpmElement:
					if m.bpm != 1 {
						m.bpm -= 1
					}
				case timeSignatureTopElement:
					if m.timeSignatureTop != 1 {
						m.timeSignatureTop -= 1
					}
				case timeSignatureBottomElement:
					if m.timeSignatureBottom != 1 {
						m.timeSignatureBottom -= 1
					}
				}

			}
		}

	case metronomeTickMsg:
		b := m.beat + 1
		m.beat = b % m.timeSignatureTop
		return m, m.metronomeTickerCmd

	case tea.WindowSizeMsg:
		m.progress.Width = msg.Width - padding*2 - 8

		if m.progress.Width > maxWidth {
			m.progress.Width = maxWidth
		}

		m.help.Width = msg.Width

		return m, nil

	case screenChangeMsg:
		m.screen = screen(msg)
		if m.screen == settingsScreen {
			m.selected = 1
			return m, m.stopTickerCmd
		} else {
			m.selected = noneElement
			return m, m.updateTickerCmd
		}

	case tickMsg:
		if m.screen == mainScreen {
			cmd := m.progress.SetPercent(1 / (float64(maxBpm) / float64(m.bpm)))
			return m, tea.Batch(tickCmd, cmd)
		}
		return m, tickCmd

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd
	}

	return m, nil
}

func (m model) View() string {
	pad := strings.Repeat(" ", padding)

	var bpmBlock string
	if m.selected == bpmElement {
		bpmBlock = termenv.String(fmt.Sprintf("BPM: %v", m.bpm)).Reverse().String()
	} else {
		bpmBlock = fmt.Sprintf("BPM: %v", m.bpm)
	}

	var activeKeyMap help.KeyMap

	switch m.screen {
	case settingsScreen:
		activeKeyMap = settingsKeyMap
	default:
		activeKeyMap = mainKeyMap
	}

	paddedHelp := strings.Split(m.help.View(activeKeyMap), "\n")
	for i := range paddedHelp {
		paddedHelp[i] = pad + paddedHelp[i]
	}

	return "\n\n" +
		// no pad here; handled by viewMetronomeBlocks()
		m.viewMetronomeBlocks() + "\n\n" +
		pad + m.progress.View() + pad + bpmBlock + "\n\n" +
		//padding for every line
		strings.Join(paddedHelp, "\n")

}

var tickCmd = tea.Tick(time.Second/time.Duration(10), func(t time.Time) tea.Msg {
	return tickMsg(t)
})

func screenChangeCmd(screen screen) tea.Cmd {
	return func() tea.Msg {
		return screenChangeMsg(screen)
	}
}

func (m model) metronomeTickerCmd() tea.Msg {
	done := make(chan bool)
	speaker.Play(beep.Seq(beepBuffer, beep.Callback(func() {
		done <- true
	})))
	<-done
	if err := beepBuffer.Seek(0); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	return metronomeTickMsg(<-m.ticker.C)
}

func (m model) updateTickerCmd() tea.Msg {
	m.ticker.Reset(time.Minute / time.Duration(m.bpm))
	m.metronomeOn = true
	return nil
}

func (m model) stopTickerCmd() tea.Msg {
	m.ticker.Stop()
	m.metronomeOn = false
	return nil
}

const metronomeFullColor = "#FFfe83"
const metronomeEmptyColor = "#606060"
const fullRune = '█'
const emptyRune = '░'
const metronomeHeight = 5

func (m model) viewMetronomeBlocks() string {

	blockWidth := ((m.progress.Width) / m.timeSignatureTop) - padding

	b := strings.Builder{}

	filledBlock := termenv.String(string(fullRune)).Foreground(termenv.ColorProfile().Color(metronomeFullColor)).String()
	emptyBlock := termenv.String(string(emptyRune)).Foreground(termenv.ColorProfile().Color(metronomeEmptyColor)).String()

	pad := strings.Repeat(" ", padding)

	for i := 1; i <= metronomeHeight; i++ {
		for i2 := 0; i2 < m.timeSignatureTop; i2++ {
			// REMEMBER: this is at the start of the loop and not at the end. pad goes before the blocks
			b.WriteString(pad)

			if m.beat == i2 {
				b.WriteString(strings.Repeat(filledBlock, blockWidth))
			} else {
				b.WriteString(strings.Repeat(emptyBlock, blockWidth))
			}
			// unTODO: remove this
			// b.WriteString(fmt.Sprintf("%v %v", m.beat, i2))
		}

		b.WriteString(strings.Repeat(" ",
			m.progress.Width-((blockWidth+padding)*m.timeSignatureTop)+padding*2))

		metronomeMiddle := metronomeHeight/2 + 1

		if metronomeMiddle == i {
			b.WriteByte('-')
		} else if metronomeMiddle-1 == i {
			if m.selected == timeSignatureTopElement {
				b.WriteString(termenv.String(fmt.Sprint(m.timeSignatureTop)).Reverse().String())
			} else {
				b.WriteString(fmt.Sprint(m.timeSignatureTop))
			}
		} else if metronomeMiddle+1 == i {
			if m.selected == timeSignatureBottomElement {
				b.WriteString(termenv.String(fmt.Sprint(m.timeSignatureBottom)).Reverse().String())
			} else {
				b.WriteString(fmt.Sprint(m.timeSignatureBottom))
			}
		}

		b.WriteByte('\n')
	}

	return b.String()
}
