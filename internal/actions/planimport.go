package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/planimport"
)

// ImportPlannerIn is the input of workspace_import_planner_dir.
type ImportPlannerIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace to import into; defaults to the active one"`
	Dir         string `json:"dir,omitempty" jsonschema:"the planner run folder; default is <workspace root>/.ai/runs/planner"`
	DryRun      *bool  `json:"dry_run,omitempty" jsonschema:"only report the plan and change nothing (default true). Pass false to import"`
}

// ImportCard is what the import does with one session file.
type ImportCard struct {
	Nick            string `json:"nick"`
	Key             string `json:"key"`
	Action          string `json:"action" jsonschema:"create or skip"`
	Reason          string `json:"reason,omitempty"`
	Column          string `json:"column"`
	Stage           string `json:"stage"`
	Liveness        string `json:"liveness"`
	ClaudeSessionID string `json:"claude_session_id"`
	Messages        int    `json:"messages"`
	Questions       int    `json:"questions"`
	OpenQuestions   int    `json:"open_questions"`
	Gates           int    `json:"gates"`
}

// ImportOut is the plan, and what was done when dry_run is false.
type ImportOut struct {
	Dir          string       `json:"dir"`
	DryRun       bool         `json:"dry_run"`
	Cards        []ImportCard `json:"cards"`
	Created      int          `json:"created"`
	Skipped      int          `json:"skipped"`
	Messages     int          `json:"messages"`
	Questions    int          `json:"questions"`
	Gates        int          `json:"gates"`
	UnmatchedLog int          `json:"unmatched_log_lines"`
	Warnings     []string     `json:"warnings"`
}

func firstWithRole(cols []domain.Column, role string) (domain.Column, bool) {
	for _, c := range cols {
		if c.Role == role {
			return c, true
		}
	}
	return domain.Column{}, false
}

func registerPlanImport(r *Registry) {
	Register(r, Spec{Name: "workspace_import_planner_dir", Description: "Import an older planner's run folder into a workspace: sessions/<nick>.md become cards (with their reports and planner messages and the Claude session id to resume), log.md sets each card's column (planning and in-progress go to the first active column with that stage, backlog to backlog, done to done), questions/<nick>/Q-NN.json become questions and gates/<nick>.json become gates. Cards are created without starting; session_start resumes the conversation with claude --resume. Nicknames that already exist are skipped, so importing twice changes nothing. dry_run (the default) only returns the plan.",
		Mutates: true, Entities: []string{"cards", "sessions", "messages", "questions", "gates"}},
		func(ctx context.Context, c *Call, in ImportPlannerIn) (ImportOut, error) {
			if err := userOrPlanner(c, "import a planner folder"); err != nil {
				return ImportOut{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return ImportOut{}, err
			}
			dry := in.DryRun == nil || *in.DryRun
			dir := strings.TrimSpace(in.Dir)
			if dir == "" {
				dir = filepath.Join(w.Root, ".ai", "runs", "planner")
			}
			if strings.HasPrefix(dir, "~/") {
				home, _ := os.UserHomeDir()
				dir = filepath.Join(home, dir[2:])
			}
			parsed, err := planimport.Parse(dir)
			if err != nil {
				return ImportOut{}, err
			}
			cols, err := c.Store.Columns.List(w.ID)
			if err != nil {
				return ImportOut{}, err
			}
			out := ImportOut{Dir: dir, DryRun: dry, Cards: []ImportCard{}, UnmatchedLog: parsed.UnmatchedLog, Warnings: []string{}}
			warned := map[string]bool{}
			warn := func(s string) {
				if !warned[s] {
					warned[s] = true
					out.Warnings = append(out.Warnings, s)
				}
			}
			columnFor := func(role string) domain.Column {
				if col, ok := firstWithRole(cols, role); ok {
					return col
				}
				warn(fmt.Sprintf("this workspace has no column with the role %s, so those cards go to %s", role, cols[0].Name))
				return cols[0]
			}
			sort.Slice(parsed.Sessions, func(i, j int) bool { return parsed.Sessions[i].Nick < parsed.Sessions[j].Nick })
			for _, s := range parsed.Sessions {
				pc := ImportCard{Nick: s.Nick, Key: s.Key, ClaudeSessionID: s.ClaudeSessionID, Messages: len(s.Messages), Questions: len(s.Questions), Gates: len(s.Gates)}
				for _, p := range s.Problems {
					warn(s.Nick + ": " + p)
				}
				if !planimport.ValidNick(s.Nick) {
					pc.Action, pc.Reason = "skip", "the file name is not a kebab-case nickname"
				} else if _, err := c.Store.Cards.ByNick(w.ID, s.Nick); err == nil {
					pc.Action, pc.Reason = "skip", "a card with this nickname already exists"
				}
				if pc.Action == "skip" {
					out.Skipped++
					out.Cards = append(out.Cards, pc)
					continue
				}
				pc.Action = "create"
				var col domain.Column
				switch s.Target {
				case "backlog":
					col = columnFor(domain.RoleBacklog)
				case "done":
					col = columnFor(domain.RoleDone)
				default:
					col = columnFor(domain.RoleActive)
				}
				if s.Target == "planning" || s.Target == "in-progress" {
					pc.Stage = s.Target
				}
				returnCol := ""
				for _, q := range s.Questions {
					if q.Status == "open" {
						pc.OpenQuestions++
					}
				}
				pc.Liveness = domain.LivenessIdle
				if pc.OpenQuestions > 0 || len(s.Gates) > 0 {
					if needs, ok := firstWithRole(cols, domain.RoleNeedsYou); ok {
						if needs.ID != col.ID {
							returnCol = col.ID
						}
						col = needs
					}
					if pc.OpenQuestions > 0 {
						pc.Liveness = domain.LivenessWaiting
					}
				}
				pc.Column = col.Name
				out.Created++
				out.Messages += len(s.Messages)
				out.Questions += len(s.Questions)
				out.Gates += len(s.Gates)
				out.Cards = append(out.Cards, pc)
				if dry {
					continue
				}
				if err := applyImported(c, w, s, pc, col.ID, returnCol); err != nil {
					return ImportOut{}, fmt.Errorf("importing %s: %w", s.Nick, err)
				}
			}
			return out, nil
		})
}

func applyImported(c *Call, w domain.Workspace, s *planimport.Session, pc ImportCard, columnID, returnColumnID string) error {
	card := domain.Card{WorkspaceID: w.ID, ColumnID: columnID, Nick: s.Nick, Key: s.Key, Assignee: "You", Stage: pc.Stage, Liveness: pc.Liveness, ReturnColumnID: returnColumnID}
	card.CreatedAt = s.Created
	if err := c.Store.Cards.Create(&card); err != nil {
		return nickError(err, s.Nick, w.Name)
	}
	if s.ClaudeSessionID != "" {
		row := domain.Session{WorkspaceID: w.ID, CardID: card.ID, Kind: "card", Command: "claude", Cwd: w.Root, Status: "exited", ClaudeSessionID: s.ClaudeSessionID}
		if err := c.Store.Sessions.Create(&row); err != nil {
			return err
		}
	}
	channel := chat.CardChannel(card.ID)
	for _, m := range s.Messages {
		msg := domain.Message{WorkspaceID: w.ID, Channel: channel, Text: m.Text, Delivered: true}
		msg.CreatedAt = m.At
		if m.FromSession {
			msg.Author, msg.Kind, msg.ReportKind = "session", "report", reportKindOf(m.Kind)
		} else {
			msg.Author, msg.Kind = "planner", "text"
		}
		if err := c.Store.Messages.Create(&msg); err != nil {
			return err
		}
	}
	for _, q := range s.Questions {
		row := domain.Question{WorkspaceID: w.ID, CardID: card.ID, Kind: q.Kind, Text: q.Text, Context: q.Context, Multi: q.Multi, Status: q.Status, Approval: q.Approval}
		row.CreatedAt = q.AskedAt
		for _, o := range q.Options {
			row.Options = append(row.Options, domain.QuestionOption{Label: o.Label, Description: o.Description})
		}
		if q.Status == "answered" {
			sel := q.Selections
			if sel == nil {
				sel = []string{}
			}
			row.Answer, _ = json.Marshal(map[string]any{"selections": sel, "notes": q.Notes, "via": q.Via})
			if !q.AnsweredAt.IsZero() {
				at := q.AnsweredAt
				row.AnsweredAt = &at
			}
		}
		if err := c.Store.Questions.Create(&row); err != nil {
			return err
		}
	}
	for _, g := range s.Gates {
		row := domain.Gate{WorkspaceID: w.ID, CardID: card.ID, Kind: g.Kind, Summary: g.Summary, Since: g.Since}
		if err := c.Store.Gates.Create(&row); err != nil {
			return err
		}
	}
	return nil
}

func reportKindOf(kind string) string {
	for _, k := range reportKinds {
		if k == kind {
			return kind
		}
	}
	return "progress"
}
