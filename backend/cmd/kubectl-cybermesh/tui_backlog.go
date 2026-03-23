package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type backlogModel struct {
	ctx          context.Context
	fetch        backlogFetcher
	autoExit     bool
	loading      bool
	data         outboxBacklogResult
	err          error
	showHelp     bool
	scrollOffset int
	width        int
	height       int
}

func newBacklogModel(ctx context.Context, fetch backlogFetcher) tea.Model {
	width, height := initialTerminalSize()
	return &backlogModel{
		ctx:      ctx,
		fetch:    fetch,
		autoExit: tuiAutoExitEnabled(),
		loading:  true,
		width:    width,
		height:   height,
	}
}

func (m *backlogModel) Init() tea.Cmd { return asyncBacklogLoad(m.ctx, m.fetch) }

func (m *backlogModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		if m.showHelp {
			switch msg.String() {
			case "?", "esc":
				m.showHelp = false
			}
			return m, nil
		}
		if handleViewportKey(msg, m.height, &m.scrollOffset) {
			return m, nil
		}
		switch msg.String() {
		case "?":
			m.showHelp = true
			return m, nil
		case "ctrl+c", "q":
			return m, tea.Quit
		case "r":
			m.loading = true
			m.err = nil
			m.scrollOffset = 0
			return m, asyncBacklogLoad(m.ctx, m.fetch)
		}
	case backlogLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.data = msg.data
		}
		if m.autoExit {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *backlogModel) View() string {
	compact := compactLayout(m.width)
	header := []string{
		titleStyle.Render("CyberMesh Backlog"),
		helpStyle.Render(backlogHelpText(compact)),
		"",
	}
	if m.loading {
		return joinLines(append(header, "Loading backlog...")...)
	}
	if m.showHelp {
		return renderHelpOverlay("Backlog Help", []string{
			"r: refresh backlog metrics",
			"PgUp / PgDn: scroll the full detail body",
			"Ctrl+U / Ctrl+D: faster full-body scroll",
			"q: quit",
		}, m.height)
	}
	if m.err != nil {
		header = append(header, errorStyle.Render(m.err.Error()))
	}
	body := []string{
		labelStyle.Render("Queue Health"),
		kvLine("Pending", fmt.Sprintf("%d", m.data.Pending)),
		kvLine("Retry", fmt.Sprintf("%d", m.data.Retry)),
		kvLine("Publishing", fmt.Sprintf("%d", m.data.Publishing)),
		kvLine("Published Rows", fmt.Sprintf("%d", m.data.PublishedRows)),
		kvLine("Acked Rows", fmt.Sprintf("%d", m.data.AckedRows)),
		kvLine("Terminal Rows", fmt.Sprintf("%d", m.data.TerminalRows)),
		kvLine("Total Rows", fmt.Sprintf("%d", m.data.TotalRows)),
		"",
		labelStyle.Render("Latency And Closure"),
		kvLine("Oldest Pending Age", formatDurationMillis(m.data.OldestPendingAgeMs)),
		kvLine("ACK Closure Ratio", fmt.Sprintf("%.2f", m.data.AckClosureRatio)),
	}
	if !compact {
		body = append(body,
			"",
			labelStyle.Render("Operational Read"),
			healthHintLine("Pending backlog", m.data.Pending == 0, fmt.Sprintf("%d rows pending", m.data.Pending), "no rows pending"),
			healthHintLine("Retry pressure", m.data.Retry == 0, fmt.Sprintf("%d rows retrying", m.data.Retry), "no retry pressure"),
			healthHintLine("Publisher activity", m.data.Publishing > 0, fmt.Sprintf("%d rows publishing", m.data.Publishing), "publisher idle"),
		)
	}
	return finalizeView(header, body, m.height, m.scrollOffset)
}

func backlogHelpText(compact bool) string {
	if compact {
		return "r refresh • ? help • pg scroll • q quit"
	}
	return "r refresh • ? help • pgup/pgdn scroll • q quit"
}

func healthHintLine(label string, warn bool, active, quiet string) string {
	if warn {
		return kvLine(label, active)
	}
	return kvLine(label, quiet)
}
