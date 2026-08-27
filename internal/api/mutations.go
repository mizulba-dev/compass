package api

import (
	"encoding/json"
	"time"
)

// setGoalRequest backs POST /api/canvas/:id/goal (the set_goal WebMCP tool).
type setGoalRequest struct {
	tokenEnvelope
	Title    string `json:"title"`
	Deadline string `json:"deadline,omitempty"`
	Why      string `json:"why,omitempty"`
}

func applyGoal(canvas *Canvas, body json.RawMessage) string {
	var req setGoalRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	if req.Title == "" {
		return "title is required"
	}
	canvas.Goal = &Goal{Title: req.Title, Deadline: req.Deadline, Why: req.Why}
	return ""
}

// setCurrentRequest backs POST /api/canvas/:id/current (set_current).
type setCurrentRequest struct {
	tokenEnvelope
	Summary string `json:"summary"`
}

func applyCurrent(canvas *Canvas, body json.RawMessage) string {
	var req setCurrentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	if req.Summary == "" {
		return "summary is required"
	}
	canvas.Current = &Current{Summary: req.Summary, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	return ""
}

// upsertGapsRequest backs POST /api/canvas/:id/gaps (upsert_gaps): add new
// gap texts and/or mark existing gap ids resolved.
type upsertGapsRequest struct {
	tokenEnvelope
	Add     []string `json:"add,omitempty"`
	Resolve []string `json:"resolve,omitempty"`
}

func applyGaps(canvas *Canvas, body json.RawMessage) string {
	var req upsertGapsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	resolved := make(map[string]bool, len(req.Resolve))
	for _, id := range req.Resolve {
		resolved[id] = true
	}
	for i := range canvas.Gaps {
		if resolved[canvas.Gaps[i].ID] {
			canvas.Gaps[i].Resolved = true
		}
	}
	for _, text := range req.Add {
		if text == "" {
			continue
		}
		canvas.Gaps = append(canvas.Gaps, Gap{ID: newLocalID(), Text: text, Resolved: false})
	}
	return ""
}

// planTasksRequest backs POST /api/canvas/:id/tasks/plan (plan_tasks). Every
// task in Tasks is treated as newly proposed by the agent: existing tasks
// (matched by id) keep their persisted order, and only tasks with unknown or
// empty ids are appended at the end in the given sequence.
type planTasksRequest struct {
	tokenEnvelope
	Tasks []planTaskInput `json:"tasks"`
}

type planTaskInput struct {
	ID   string  `json:"id,omitempty"`
	Text string  `json:"text"`
	Day  *string `json:"day,omitempty"`
}

// applyPlanTasks preserves the persisted order of every existing task
// (matched by id): only tasks whose id does not already exist are appended,
// in the order given, after the current maximum order.
func applyPlanTasks(canvas *Canvas, body json.RawMessage) string {
	var req planTasksRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}

	existingByID := make(map[string]int, len(canvas.Tasks))
	maxOrder := -1
	for i, t := range canvas.Tasks {
		existingByID[t.ID] = i
		if t.Order > maxOrder {
			maxOrder = t.Order
		}
	}

	for _, in := range req.Tasks {
		if in.Text == "" {
			continue
		}
		if in.ID != "" {
			if idx, ok := existingByID[in.ID]; ok {
				// Existing task: update content only, order untouched.
				canvas.Tasks[idx].Text = in.Text
				canvas.Tasks[idx].Day = in.Day
				continue
			}
		}
		maxOrder++
		canvas.Tasks = append(canvas.Tasks, Task{
			ID:     newLocalID(),
			Text:   in.Text,
			Day:    in.Day,
			Order:  maxOrder,
			Done:   false,
			Origin: "agent",
		})
	}
	return ""
}

// updateTasksRequest backs POST /api/canvas/:id/tasks/update (update_tasks).
// This is the agent-facing WebMCP tool's endpoint and only ever accepts
// Text/Done — reordering and deletion have no fields here at all, so there
// is no server-side surface for an agent to move or remove a task even if
// it ignores its tool's inputSchema.
type updateTasksRequest struct {
	tokenEnvelope
	Updates []taskUpdateInput `json:"updates"`
}

type taskUpdateInput struct {
	ID   string  `json:"id"`
	Text *string `json:"text,omitempty"`
	Done *bool   `json:"done,omitempty"`
}

func applyUpdateTasks(canvas *Canvas, body json.RawMessage) string {
	var req updateTasksRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	byID := make(map[string]int, len(canvas.Tasks))
	for i, t := range canvas.Tasks {
		byID[t.ID] = i
	}
	for _, u := range req.Updates {
		idx, ok := byID[u.ID]
		if !ok {
			continue
		}
		if u.Text != nil {
			canvas.Tasks[idx].Text = *u.Text
		}
		if u.Done != nil {
			canvas.Tasks[idx].Done = *u.Done
		}
	}
	return ""
}

// humanTaskEditRequest backs POST /api/canvas/:id/tasks/human. Reordering
// and deletion are human-only affordances driven by direct canvas edits on
// the page; they have no WebMCP tool and no path through /tasks/update.
type humanTaskEditRequest struct {
	tokenEnvelope
	Edits []humanTaskEditInput `json:"edits"`
}

type humanTaskEditInput struct {
	ID     string `json:"id"`
	Order  *int   `json:"order,omitempty"`
	Delete bool   `json:"delete,omitempty"`
}

func applyHumanTaskEdit(canvas *Canvas, body json.RawMessage) string {
	var req humanTaskEditRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	byID := make(map[string]int, len(canvas.Tasks))
	for i, t := range canvas.Tasks {
		byID[t.ID] = i
	}
	toDelete := make(map[string]bool)
	for _, e := range req.Edits {
		idx, ok := byID[e.ID]
		if !ok {
			continue
		}
		if e.Delete {
			toDelete[e.ID] = true
			continue
		}
		if e.Order != nil {
			canvas.Tasks[idx].Order = *e.Order
		}
	}
	if len(toDelete) > 0 {
		kept := canvas.Tasks[:0]
		for _, t := range canvas.Tasks {
			if !toDelete[t.ID] {
				kept = append(kept, t)
			}
		}
		canvas.Tasks = kept
	}
	return ""
}

// addPolicyRequest backs POST /api/canvas/:id/policies (add_policy).
type addPolicyRequest struct {
	tokenEnvelope
	Text        string `json:"text"`
	DerivedFrom string `json:"derivedFrom"`
}

func applyPolicy(canvas *Canvas, body json.RawMessage) string {
	var req addPolicyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "invalid JSON body"
	}
	if req.Text == "" {
		return "text is required"
	}
	canvas.Policies = append(canvas.Policies, Policy{
		ID:          newLocalID(),
		Text:        req.Text,
		DerivedFrom: req.DerivedFrom,
	})
	return ""
}
