// Package domain holds Hadron's plain types. They carry JSON tags because the same structs are the
// action registry's inputs and outputs, so they travel unchanged to the frontend and to MCP.
package domain

import (
	"encoding/json"
	"time"
)

// Column roles. Rules use the role, never the name, so a column can be renamed freely.
const (
	RoleNeedsYou = "needs-you"
	RoleActive   = "active"
	RoleBlocked  = "blocked"
	RoleDone     = "done"
	RoleBacklog  = "backlog"
)

// Roles lists every valid column role, in display order.
var Roles = []string{RoleNeedsYou, RoleActive, RoleBlocked, RoleDone, RoleBacklog}

// Liveness values of a card's session.
const (
	LivenessWorking = "working"
	LivenessIdle    = "idle"
	LivenessWaiting = "waiting"
	LivenessEnded   = "ended"
)

// Meta is on every row.
type Meta struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Workspace is a name, a colour, a root folder and its board.
type Workspace struct {
	Meta
	Name         string         `json:"name"`
	Color        string         `json:"color"`
	Root         string         `json:"root"`
	Position     int            `json:"position"`
	Archived     bool           `json:"archived"`
	Integrations []string       `json:"integrations"`
	FilterBar    []string       `json:"filter_bar"`
	UI           map[string]any `json:"ui"`
	// DefaultModel is the Claude model cards use when neither the card nor its launch profile names
	// one. PlannerModel is the planner's. Empty means no --model flag.
	DefaultModel string `json:"default_model"`
	PlannerModel string `json:"planner_model"`
	// PlannerCwd is the folder the planner's session runs in. Empty means Root.
	PlannerCwd string `json:"planner_cwd"`
}

// Column is one board column.
type Column struct {
	Meta
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	Position    int    `json:"position"`
	WIP         int    `json:"wip"`
	Fold        bool   `json:"fold"`
	Color       string `json:"color"`
}

// Card is one Claude Code session on the board.
type Card struct {
	Meta
	WorkspaceID    string         `json:"workspace_id"`
	ColumnID       string         `json:"column_id"`
	Position       int            `json:"position"`
	Nick           string         `json:"nick"`
	Key            string         `json:"key"`
	Title          string         `json:"title"`
	Assignee       string         `json:"assignee"`
	Stage          string         `json:"stage"`
	Liveness       string         `json:"liveness"`
	ReturnColumnID string         `json:"return_column_id"`
	LaunchProfile  string         `json:"launch_profile"`
	Fields         map[string]any `json:"fields"`
	Archived       bool           `json:"archived"`
	// Prompt is the card layer of its session prompt.
	Prompt string `json:"prompt"`
	// Model is the Claude model this card's session runs; empty falls back to the launch profile's,
	// then the workspace default.
	Model string `json:"model"`
	// Cwd is the folder the card's session runs in (a launch profile's worktree is made from it).
	// Empty means the workspace root.
	Cwd string `json:"cwd"`
}

// Session is one PTY process: a card's, or a workspace planner's (CardID empty).
type Session struct {
	Meta
	WorkspaceID     string     `json:"workspace_id"`
	CardID          string     `json:"card_id"`
	Kind            string     `json:"kind"`
	Command         string     `json:"command"`
	Cwd             string     `json:"cwd"`
	PID             int        `json:"pid"`
	Status          string     `json:"status"`
	ExitCode        int        `json:"exit_code"`
	ClaudeSessionID string     `json:"claude_session_id"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`
	// Restore means the session is meant to be running: Hadron brings it back after a restart,
	// an update or a crash. It is cleared when the session ends on its own or the user stops it.
	Restore bool `json:"restore"`
}

// MessageCommand is the kind of a slash command the user sent from a chat straight to a session's
// terminal (chat_command). The planner never hears it.
const MessageCommand = "command"

// Message is one line of a chat channel ("planner" or "card:<id>").
type Message struct {
	Meta
	WorkspaceID string `json:"workspace_id"`
	Channel     string `json:"channel"`
	Author      string `json:"author"`
	Kind        string `json:"kind"`
	ReportKind  string `json:"report_kind"`
	Text        string `json:"text"`
	WidgetTitle string `json:"widget_title"`
	WidgetHTML  string `json:"widget_html"`
	Delivered   bool   `json:"delivered"`
	Reason      string `json:"reason"`
	// QuestionID links a question's message and its answer to the question.
	QuestionID string `json:"question_id"`
}

// QuestionOption is one choice of a question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Question is a decision, unblock or approval a session asks about its own card.
type Question struct {
	Meta
	WorkspaceID string           `json:"workspace_id"`
	CardID      string           `json:"card_id"`
	Seq         int              `json:"seq"`
	Kind        string           `json:"kind"`
	Text        string           `json:"text"`
	Context     string           `json:"context"`
	Options     []QuestionOption `json:"options"`
	Multi       bool             `json:"multi"`
	Approval    json.RawMessage  `json:"approval"`
	Status      string           `json:"status"`
	Answer      json.RawMessage  `json:"answer"`
	AnsweredAt  *time.Time       `json:"answered_at"`
	// Meanwhile is what the session does while the question waits.
	Meanwhile string `json:"meanwhile"`
}

// Gate is a merge, promote, deploy-confirm or close waiting on the user.
type Gate struct {
	Meta
	WorkspaceID string    `json:"workspace_id"`
	CardID      string    `json:"card_id"`
	Kind        string    `json:"kind"`
	Summary     string    `json:"summary"`
	Since       time.Time `json:"since"`
}

// Link ties a card to an item in an integration.
type Link struct {
	Meta
	WorkspaceID string         `json:"workspace_id"`
	CardID      string         `json:"card_id"`
	Integration string         `json:"integration"`
	ItemID      string         `json:"item_id"`
	Title       string         `json:"title"`
	URL         string         `json:"url"`
	Fields      map[string]any `json:"fields"`
	SyncedAt    *time.Time     `json:"synced_at"`
}

// Rule reacts to a trigger with actions.
type Rule struct {
	Meta
	WorkspaceID string          `json:"workspace_id"`
	Position    int             `json:"position"`
	Name        string          `json:"name"`
	Enabled     bool            `json:"enabled"`
	Trigger     json.RawMessage `json:"trigger"`
	Condition   string          `json:"condition"`
	Actions     json.RawMessage `json:"actions"`
	Builtin     bool            `json:"builtin"`
}

// Routine sends a prompt to the planner or a card's session on a schedule: a five-field cron spec
// in local time, or every N minutes. A stopped session is started and gets the prompt once ready.
type Routine struct {
	Meta
	WorkspaceID  string `json:"workspace_id"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	Cron         string `json:"cron"`
	EveryMinutes int    `json:"every_minutes"`
	// Target is planner or card; CardID names the card.
	Target string `json:"target"`
	CardID string `json:"card_id"`
	Prompt string `json:"prompt"`
	// CatchUp runs a time missed while Hadron was closed once, when it opens; off skips it.
	CatchUp    bool       `json:"catch_up"`
	NextRunAt  *time.Time `json:"next_run_at"`
	LastRunAt  *time.Time `json:"last_run_at"`
	LastResult string     `json:"last_result"`
}

// Routine targets.
const (
	RoutinePlanner = "planner"
	RoutineCard    = "card"
)

// View is a saved filter.
type View struct {
	Meta
	WorkspaceID string          `json:"workspace_id"`
	Name        string          `json:"name"`
	Filters     json.RawMessage `json:"filters"`
	Position    int             `json:"position"`
}

// Block is one dashboard block.
type Block struct {
	Meta
	WorkspaceID string          `json:"workspace_id"`
	Position    int             `json:"position"`
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Span        int             `json:"span"`
	Source      json.RawMessage `json:"source"`
	HTML        string          `json:"html"`
	Schedule    string          `json:"schedule"`
	Text        string          `json:"text"`
	TextAt      *time.Time      `json:"text_at"`
}

// Prompt is a workspace's override of a default prompt. A nil Template means the default.
type Prompt struct {
	Meta
	WorkspaceID string  `json:"workspace_id"`
	Kind        string  `json:"kind"`
	Template    *string `json:"template"`
}

// LaunchProfile is a saved recipe for how a session starts.
type LaunchProfile struct {
	Meta
	WorkspaceID string            `json:"workspace_id"`
	Name        string            `json:"name"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	FirstPrompt string            `json:"first_prompt"`
	Env         map[string]string `json:"env"`
	Worktree    bool              `json:"worktree"`
	// Prompt is the launch profile layer of the session prompt.
	Prompt string `json:"prompt"`
	// Model is the Claude model sessions of this profile run, unless their card names one.
	Model string `json:"model"`
}

// Script is an integration or a user script. WorkspaceID empty means global.
type Script struct {
	Meta
	WorkspaceID string          `json:"workspace_id"`
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Manifest    json.RawMessage `json:"manifest"`
	Source      string          `json:"source"`
	Status      string          `json:"status"`
	ApprovedBy  string          `json:"approved_by"`
	ApprovedAt  *time.Time      `json:"approved_at"`
	TestResult  json.RawMessage `json:"test_result"`
}

// Change is one config change with its before and after, so it can be reverted.
type Change struct {
	Meta
	WorkspaceID string          `json:"workspace_id"`
	Actor       string          `json:"actor"`
	Action      string          `json:"action"`
	Args        json.RawMessage `json:"args"`
	Before      json.RawMessage `json:"before"`
	After       json.RawMessage `json:"after"`
	Reverted    bool            `json:"reverted"`
}

// MCPCall is one logged tool call, from the UI or from MCP.
type MCPCall struct {
	Meta
	WorkspaceID string          `json:"workspace_id"`
	Actor       string          `json:"actor"`
	Tool        string          `json:"tool"`
	Args        json.RawMessage `json:"args"`
	OK          bool            `json:"ok"`
	Error       string          `json:"error"`
	DurationMS  int             `json:"duration_ms"`
}

// ScriptLog is one op run of an integration or script, or a line its code logged.
type ScriptLog struct {
	Meta
	WorkspaceID string `json:"workspace_id"`
	ScriptName  string `json:"script_name"`
	Op          string `json:"op"`
	OK          bool   `json:"ok"`
	DurationMS  int    `json:"duration_ms"`
	Message     string `json:"message"`
}

// Feedback is one note about Hadron itself in the feedback inbox.
type Feedback struct {
	Meta
	Kind        string          `json:"kind"`
	Text        string          `json:"text"`
	Area        string          `json:"area"`
	Version     string          `json:"version"`
	Diagnostics json.RawMessage `json:"diagnostics"`
	Status      string          `json:"status"`
	Note        string          `json:"note"`
	GithubURL   string          `json:"github_url"`
}
