package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type aiTab string

const (
	aiTabHistory aiTab = "history"
	aiTabNodes   aiTab = "suspicious-nodes"
)

type aiModel struct {
	ctx          context.Context
	historyFetch aiHistoryFetcher
	nodesFetch   aiSuspiciousNodesFetcher
	autoExit     bool
	tab          aiTab
	loading      bool
	history      aiHistoryResult
	nodes        aiSuspiciousNodesResult
	selected     int
	err          error
	showHelp     bool
	detailMode   bool
	scrollOffset int
	width        int
	height       int
}

func newAIModel(ctx context.Context, initial aiTab, historyFetch aiHistoryFetcher, nodesFetch aiSuspiciousNodesFetcher) tea.Model {
	width, height := initialTerminalSize()
	return &aiModel{
		ctx:          ctx,
		historyFetch: historyFetch,
		nodesFetch:   nodesFetch,
		autoExit:     tuiAutoExitEnabled(),
		tab:          initial,
		loading:      true,
		width:        width,
		height:       height,
	}
}

func (m *aiModel) Init() tea.Cmd {
	return tea.Batch(
		asyncAIHistoryLoad(m.ctx, m.historyFetch),
		asyncAISuspiciousNodesLoad(m.ctx, m.nodesFetch),
	)
}

func (m *aiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			return m, tea.Batch(
				asyncAIHistoryLoad(m.ctx, m.historyFetch),
				asyncAISuspiciousNodesLoad(m.ctx, m.nodesFetch),
			)
		case "tab":
			m.toggleTab()
			return m, nil
		case "enter":
			m.detailMode = !m.detailMode
			m.scrollOffset = 0
			return m, nil
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.scrollOffset = 0
			}
		case "down", "j":
			if m.selected < m.activeCount()-1 {
				m.selected++
				m.scrollOffset = 0
			}
		}
	case aiHistoryLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.history = msg.data
		}
		m.finishLoad()
	case aiSuspiciousNodesLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.nodes = msg.data
		}
		m.finishLoad()
	}
	return m, nil
}

func (m *aiModel) finishLoad() {
	if m.history.UpdatedAt == "" && m.nodes.UpdatedAt == "" && m.err == nil {
		return
	}
	m.loading = false
	if m.selected >= m.activeCount() {
		m.selected = maxInt(0, m.activeCount()-1)
	}
}

func (m *aiModel) toggleTab() {
	if m.tab == aiTabHistory {
		m.tab = aiTabNodes
	} else {
		m.tab = aiTabHistory
	}
	m.selected = 0
	m.scrollOffset = 0
}

func (m *aiModel) activeCount() int {
	if m.tab == aiTabHistory {
		return len(m.history.Detections)
	}
	return len(m.nodes.Nodes)
}

func (m *aiModel) selectedDetection() *aiHistoryDetection {
	if m.tab != aiTabHistory || m.selected < 0 || m.selected >= len(m.history.Detections) {
		return nil
	}
	return &m.history.Detections[m.selected]
}

func (m *aiModel) selectedNode() *aiSuspiciousNode {
	if m.tab != aiTabNodes || m.selected < 0 || m.selected >= len(m.nodes.Nodes) {
		return nil
	}
	return &m.nodes.Nodes[m.selected]
}

func (m *aiModel) View() string {
	compact := compactLayout(m.width)
	header := []string{
		titleStyle.Render("CyberMesh AI"),
		helpStyle.Render(aiHelpText(compact, m.tab)),
		"",
		kvLine("View", string(m.tab)),
		kvLine("History Count", fmt.Sprintf("%d", m.history.Count)),
		kvLine("Suspicious Nodes", fmt.Sprintf("%d", len(m.nodes.Nodes))),
	}
	if m.loading {
		return joinLines(append(header, "", "Loading AI views...")...)
	}
	if m.showHelp {
		return renderHelpOverlay("AI Help", []string{
			"tab: switch between history and suspicious nodes",
			"j/k or arrows: move selected row",
			"enter: expand or collapse detail",
			"PgUp / PgDn: scroll the full detail body",
			"Ctrl+U / Ctrl+D: faster full-body scroll",
			"r: refresh",
			"q: quit",
		}, m.height)
	}
	if m.err != nil {
		header = append(header, "", errorStyle.Render(m.err.Error()))
	}
	body := []string{""}
	if m.tab == aiTabHistory {
		body = append(body, renderAIDetectionPane(m.history.Detections, m.selected, listRowsForHeight(m.height), compact)...)
		body = append(body, "", labelStyle.Render("Selected Detection"))
		if row := m.selectedDetection(); row != nil {
			body = append(body,
				kvLine("ID", row.ID),
				kvLine("Severity", renderStatus(row.Severity)),
				kvLine("Type", row.Type),
				kvLine("Confidence", fmt.Sprintf("%.2f", row.Confidence)),
				kvLine("Policy", row.PolicyID),
				kvLine("Workflow", row.WorkflowID),
				kvLine("Trace", row.TraceID),
				kvLine("Anomaly", row.AnomalyID),
				kvLine("Sentinel Event", row.SentinelEventID),
				kvLine("Source", row.Source),
				kvLine("When", formatTimestampAuto(row.Timestamp)),
			)
			if m.detailMode {
				body = append(body,
					kvLine("Title", row.Title),
					kvLine("Description", row.Description),
				)
			}
		} else {
			body = append(body, "No detections available.")
		}
	} else {
		body = append(body, renderAISuspiciousPane(m.nodes.Nodes, m.selected, listRowsForHeight(m.height), compact)...)
		body = append(body, "", labelStyle.Render("Selected Node"))
		if row := m.selectedNode(); row != nil {
			body = append(body,
				kvLine("Node", row.ID),
				kvLine("Status", renderStatus(row.Status)),
				kvLine("Uptime", fmt.Sprintf("%.2f", row.Uptime)),
				kvLine("Suspicion Score", fmt.Sprintf("%.2f", row.SuspicionScore)),
				kvLine("Reason", row.Reason),
			)
		} else {
			body = append(body, "No suspicious nodes reported.")
		}
	}
	return finalizeView(header, body, m.height, m.scrollOffset)
}

func aiHelpText(compact bool, tab aiTab) string {
	active := "history"
	if tab == aiTabNodes {
		active = "nodes"
	}
	if compact {
		return fmt.Sprintf("tab switch(%s) • j/k move • enter detail • ? help • pg scroll • q quit", active)
	}
	return fmt.Sprintf("tab switch(%s) • j/k move • enter detail • r refresh • pgup/pgdn scroll • ? help • q quit", active)
}

func renderAIDetectionPane(rows []aiHistoryDetection, selected int, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render("History")}
	if len(rows) == 0 {
		return append(lines, "- no detections -")
	}
	start, end := listWindow(len(rows), selected, maxRows)
	if start > 0 {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d earlier rows ...", start)))
	}
	for i := start; i < end; i++ {
		row := rows[i]
		prefix := " "
		if i == selected {
			prefix = cursorStyle.String()
		}
		if compact {
			lines = append(lines, fmt.Sprintf("%s %s  %s", prefix, blankDash(shortID(row.ID, 16)), renderStatus(row.Severity)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  %s  conf=%.2f", prefix, blankDash(shortID(row.ID, 18)), renderStatus(row.Severity), blankDash(row.Type), row.Confidence))
	}
	if end < len(rows) {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more rows ...", len(rows)-end)))
	}
	return lines
}

func renderAISuspiciousPane(rows []aiSuspiciousNode, selected int, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render("Suspicious Nodes")}
	if len(rows) == 0 {
		return append(lines, "- no nodes -")
	}
	start, end := listWindow(len(rows), selected, maxRows)
	if start > 0 {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d earlier rows ...", start)))
	}
	for i := start; i < end; i++ {
		row := rows[i]
		prefix := " "
		if i == selected {
			prefix = cursorStyle.String()
		}
		if compact {
			lines = append(lines, fmt.Sprintf("%s %s  %.2f", prefix, blankDash(shortID(row.ID, 16)), row.SuspicionScore))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  score=%.2f", prefix, blankDash(shortID(row.ID, 18)), renderStatus(row.Status), row.SuspicionScore))
	}
	if end < len(rows) {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more rows ...", len(rows)-end)))
	}
	return lines
}
