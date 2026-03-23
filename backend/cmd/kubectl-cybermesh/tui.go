package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type traceFetcher func(context.Context) (traceResponse, error)
type monitorFetcher func(context.Context) (monitorData, error)
type policyListFetcher func(context.Context) (policyListResult, error)
type policyDetailFetcher func(context.Context, string) (policyDetailResult, error)
type policyCoverageFetcher func(context.Context, string) (policyCoverageResult, error)
type policyMutationFetcher func(context.Context, string, string, string, actionDraft) (controlMutationResponse, error)
type policyAuditFetcher func(context.Context, string) (auditListResult, error)
type workflowListFetcher func(context.Context) (workflowListResult, error)
type workflowDetailFetcher func(context.Context, string) (workflowDetailResult, error)
type workflowRollbackFetcher func(context.Context, string, actionDraft) (workflowRollbackResult, error)
type workflowAuditFetcher func(context.Context, string) (auditListResult, error)
type auditFetcher func(context.Context) (auditListResult, error)
type backlogFetcher func(context.Context) (outboxBacklogResult, error)
type aiHistoryFetcher func(context.Context) (aiHistoryResult, error)
type aiSuspiciousNodesFetcher func(context.Context) (aiSuspiciousNodesResult, error)
type controlStatusFetcher func(context.Context) (controlStatusResponse, error)
type controlToggleFetcher func(context.Context, string, bool, actionDraft) error

type monitorData struct {
	Consensus consensusOverview  `json:"consensus"`
	Outbox    outboxListResponse `json:"outbox"`
	Acks      ackListResponse    `json:"acks"`
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	labelStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	errorStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).SetString(">")
)

const tuiListMaxRows = 8

const (
	compactWidthThreshold = 110
	tinyWidthThreshold    = 84
)

var launchInteractiveTrace = func(model tea.Model, out io.Writer) error {
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(out)).Run()
	return err
}

var launchInteractiveMonitor = func(model tea.Model, out io.Writer) error {
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(out)).Run()
	return err
}

var launchInteractivePolicies = func(model tea.Model, out io.Writer) error {
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(out)).Run()
	return err
}

var launchInteractiveWorkflows = func(model tea.Model, out io.Writer) error {
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(out)).Run()
	return err
}

var launchInteractiveAudit = func(model tea.Model, out io.Writer) error {
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(out)).Run()
	return err
}

var launchInteractiveControl = func(model tea.Model, out io.Writer) error {
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(out)).Run()
	return err
}

var launchInteractiveBacklog = func(model tea.Model, out io.Writer) error {
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(out)).Run()
	return err
}

var launchInteractiveAI = func(model tea.Model, out io.Writer) error {
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(out)).Run()
	return err
}

type traceLoadedMsg struct {
	data traceResponse
	err  error
}

type monitorLoadedMsg struct {
	data monitorData
	err  error
}

type refreshTickMsg struct{}
type policyListLoadedMsg struct {
	data policyListResult
	err  error
}
type policyDetailLoadedMsg struct {
	data policyDetailResult
	err  error
}
type policyCoverageLoadedMsg struct {
	data policyCoverageResult
	err  error
}
type policyMutationLoadedMsg struct {
	action string
	data   controlMutationResponse
	err    error
}
type workflowListLoadedMsg struct {
	data workflowListResult
	err  error
}
type workflowDetailLoadedMsg struct {
	data workflowDetailResult
	err  error
}
type workflowRollbackLoadedMsg struct {
	data workflowRollbackResult
	err  error
}
type auditLoadedMsg struct {
	data auditListResult
	err  error
}
type controlLoadedMsg struct {
	data controlStatusResponse
	err  error
}
type controlToggleLoadedMsg struct {
	kind    string
	enabled bool
	err     error
}
type backlogLoadedMsg struct {
	data outboxBacklogResult
	err  error
}
type aiHistoryLoadedMsg struct {
	data aiHistoryResult
	err  error
}
type aiSuspiciousNodesLoadedMsg struct {
	data aiSuspiciousNodesResult
	err  error
}

func asyncTraceLoad(ctx context.Context, fetch traceFetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx)
		return traceLoadedMsg{data: data, err: err}
	}
}

func asyncMonitorLoad(ctx context.Context, fetch monitorFetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx)
		return monitorLoadedMsg{data: data, err: err}
	}
}

func monitorRefreshTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

func asyncPolicyListLoad(ctx context.Context, fetch policyListFetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx)
		return policyListLoadedMsg{data: data, err: err}
	}
}

func asyncPolicyDetailLoad(ctx context.Context, fetch policyDetailFetcher, policyID string) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx, policyID)
		return policyDetailLoadedMsg{data: data, err: err}
	}
}

func asyncPolicyCoverageLoad(ctx context.Context, fetch policyCoverageFetcher, policyID string) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx, policyID)
		return policyCoverageLoadedMsg{data: data, err: err}
	}
}

func asyncPolicyMutation(ctx context.Context, fetch policyMutationFetcher, action, policyID, workflowID string, draft actionDraft) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx, action, policyID, workflowID, draft)
		return policyMutationLoadedMsg{action: action, data: data, err: err}
	}
}

func asyncWorkflowListLoad(ctx context.Context, fetch workflowListFetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx)
		return workflowListLoadedMsg{data: data, err: err}
	}
}

func asyncWorkflowDetailLoad(ctx context.Context, fetch workflowDetailFetcher, workflowID string) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx, workflowID)
		return workflowDetailLoadedMsg{data: data, err: err}
	}
}

func asyncWorkflowRollback(ctx context.Context, fetch workflowRollbackFetcher, workflowID string, draft actionDraft) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx, workflowID, draft)
		return workflowRollbackLoadedMsg{data: data, err: err}
	}
}

func asyncAuditLoad(ctx context.Context, fetch auditFetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx)
		return auditLoadedMsg{data: data, err: err}
	}
}

func asyncControlLoad(ctx context.Context, fetch controlStatusFetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx)
		return controlLoadedMsg{data: data, err: err}
	}
}

func asyncBacklogLoad(ctx context.Context, fetch backlogFetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx)
		return backlogLoadedMsg{data: data, err: err}
	}
}

func asyncAIHistoryLoad(ctx context.Context, fetch aiHistoryFetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx)
		return aiHistoryLoadedMsg{data: data, err: err}
	}
}

func asyncAISuspiciousNodesLoad(ctx context.Context, fetch aiSuspiciousNodesFetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := fetch(ctx)
		return aiSuspiciousNodesLoadedMsg{data: data, err: err}
	}
}

func asyncControlToggle(ctx context.Context, fetch controlToggleFetcher, kind string, enabled bool, draft actionDraft) tea.Cmd {
	return func() tea.Msg {
		err := fetch(ctx, kind, enabled, draft)
		return controlToggleLoadedMsg{kind: kind, enabled: enabled, err: err}
	}
}

type actionDraft struct {
	Action         string
	ReasonCode     string
	ReasonText     string
	Classification string
	Focus          int
}

func newActionDraft(action string) actionDraft {
	action = strings.ToLower(strings.TrimSpace(action))
	return actionDraft{
		Action:         action,
		ReasonCode:     "operator.interactive." + action,
		ReasonText:     "interactive " + action + " via kubectl-cybermesh",
		Classification: "interactive",
	}
}

func (d *actionDraft) activeLabel() string {
	switch d.Focus {
	case 1:
		return "reason_text"
	case 2:
		return "classification"
	default:
		return "reason_code"
	}
}

func (d *actionDraft) activeValuePtr() *string {
	switch d.Focus {
	case 1:
		return &d.ReasonText
	case 2:
		return &d.Classification
	default:
		return &d.ReasonCode
	}
}

func (d *actionDraft) handleKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyTab:
		d.Focus = (d.Focus + 1) % 3
		return true
	case tea.KeyShiftTab:
		d.Focus = (d.Focus + 2) % 3
		return true
	case tea.KeyBackspace, tea.KeyDelete:
		ptr := d.activeValuePtr()
		if len(*ptr) > 0 {
			*ptr = (*ptr)[:len(*ptr)-1]
		}
		return true
	}
	switch s := msg.String(); s {
	case "tab", "shift+tab", "enter", "y", "n", "esc", "ctrl+c", "q", "up", "down", "left", "right":
		return false
	default:
		if len(s) == 1 && s[0] >= 32 {
			ptr := d.activeValuePtr()
			*ptr += s
			return true
		}
	}
	return false
}

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n")
}

func kvLine(label, value string) string {
	return fmt.Sprintf("%s %s", labelStyle.Render(label+":"), blankDash(value))
}

func tuiAutoExitEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("CYBERMESH_TUI_AUTOEXIT")), "1") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("CYBERMESH_TUI_AUTOEXIT")), "true")
}

func tuiSnapshotEnabled() bool {
	return tuiAutoExitEnabled()
}

func tuiSmokeAction() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv("CYBERMESH_TUI_SMOKE_ACTION")))
}

func renderHelpOverlay(title string, lines []string, height int) string {
	header := []string{
		titleStyle.Render(title),
		helpStyle.Render("esc or ? closes help"),
		"",
	}
	return finalizeView(header, append([]string{}, lines...), height, 0)
}

func compactLayout(width int) bool {
	return width > 0 && width < compactWidthThreshold
}

func tinyLayout(width int) bool {
	return width > 0 && width < tinyWidthThreshold
}

func listRowsForHeight(height int) int {
	if height <= 0 {
		return tuiListMaxRows
	}
	rows := int(math.Max(4, math.Min(10, float64(height/4))))
	return rows
}

func initialTerminalSize() (int, int) {
	width, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS")))
	height, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("LINES")))
	return width, height
}

func listWindow(total, selected, maxRows int) (start, end int) {
	if maxRows <= 0 || total <= maxRows {
		return 0, total
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= total {
		selected = total - 1
	}
	half := maxRows / 2
	start = selected - half
	if start < 0 {
		start = 0
	}
	end = start + maxRows
	if end > total {
		end = total
		start = end - maxRows
	}
	return start, end
}

func viewportStep(height int) int {
	if height <= 0 {
		return 6
	}
	return maxInt(3, height/3)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func handleViewportKey(msg tea.KeyMsg, height int, offset *int) bool {
	step := viewportStep(height)
	switch msg.String() {
	case "pgdown", "ctrl+d":
		*offset += step
		return true
	case "pgup", "ctrl+u":
		*offset -= step
		if *offset < 0 {
			*offset = 0
		}
		return true
	case "home":
		*offset = 0
		return true
	}
	return false
}

func finalizeView(header, body []string, height int, offset int) string {
	if height <= 0 {
		return joinLines(append(header, body...)...)
	}
	if len(header) >= height {
		return joinLines(header[:height]...)
	}
	slots := height - len(header)
	if len(body) <= slots {
		return joinLines(append(header, body...)...)
	}
	start := clampInt(offset, 0, len(body)-1)
	topIndicator := start > 0
	reserved := -1
	for {
		currentReserved := 0
		if topIndicator {
			currentReserved++
		}
		visibleSlots := maxInt(1, slots-currentReserved)
		end := minInt(len(body), start+visibleSlots)
		bottomIndicator := end < len(body)
		newReserved := 0
		if topIndicator {
			newReserved++
		}
		if bottomIndicator {
			newReserved++
		}
		if newReserved == reserved {
			lines := append([]string{}, header...)
			if topIndicator {
				lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d lines above ...", start)))
			}
			lines = append(lines, body[start:end]...)
			if bottomIndicator {
				lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more lines ...", len(body)-end)))
			}
			return joinLines(lines...)
		}
		reserved = newReserved
	}
}
