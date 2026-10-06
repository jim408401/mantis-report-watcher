package app

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"mantis-report-watcher/internal/mantis"
)

// Group is a status bucket shown in the UI (same buckets as the old report).
type Group struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Order int    `json:"order"`
}

var groups = []Group{
	{"progress", "進行中", 1},
	{"assigned", "已分配", 2},
	{"review", "待 CR", 3},
	{"done", "已完成", 4},
	{"testing", "測試中", 5},
	{"other", "其他", 99},
}

// statusGroup mirrors the rules of the previous PowerShell report, using
// the English enum name, the localized label and finally the numeric ID.
func statusGroup(s mantis.Ref) string {
	name := strings.ToLower(s.Name + " " + s.Label)
	label := s.Name + " " + s.Label
	switch {
	case strings.Contains(name, "feedback") || strings.Contains(label, "進行中"):
		return "progress"
	case strings.Contains(name, "assigned") || strings.Contains(label, "已分配"):
		return "assigned"
	case strings.Contains(name, "review") || strings.Contains(label, "待CR") || strings.Contains(label, "待 CR"):
		return "review"
	case strings.Contains(name, "confirmed") || strings.Contains(name, "resolved") || strings.Contains(label, "完成"):
		return "done"
	case strings.Contains(name, "acknowledged") || strings.Contains(name, "test") || strings.Contains(label, "測試"):
		return "testing"
	}
	switch s.ID {
	case 20:
		return "progress"
	case 50:
		return "assigned"
	case 40, 80:
		return "done"
	case 30:
		return "testing"
	}
	return "other"
}

// NoteView is a note prepared for display.
type NoteView struct {
	Author  string    `json:"author"`
	Text    string    `json:"text"`
	Created time.Time `json:"created"`
	Private bool      `json:"private"`
}

// IssueView is an issue prepared for the UI.
type IssueView struct {
	ID             int        `json:"id"`
	URL            string     `json:"url"`
	Summary        string     `json:"summary"`
	Project        string     `json:"project"`
	Category       string     `json:"category"`
	Group          string     `json:"group"`
	StatusLabel    string     `json:"statusLabel"`
	StatusColor    string     `json:"statusColor,omitempty"`
	Priority       string     `json:"priority"`
	Severity       string     `json:"severity"`
	Reporter       string     `json:"reporter"`
	Handler        string     `json:"handler"`
	TargetVersion  string     `json:"targetVersion"`
	Created        time.Time  `json:"created"`
	Updated        time.Time  `json:"updated"`
	DueDate        *time.Time `json:"dueDate,omitempty"`
	Description    string     `json:"description"`
	Steps          string     `json:"steps,omitempty"`
	AdditionalInfo string     `json:"additionalInfo,omitempty"`
	Notes          []NoteView `json:"notes,omitempty"`
	NoteCount      int        `json:"noteCount"`
	Tags           []string   `json:"tags,omitempty"`
	IsNew          bool       `json:"isNew"`
	IsUpdated      bool       `json:"isUpdated"`
	Changes        []string   `json:"changes,omitempty"`
}

func visibleNotes(is mantis.Issue) []mantis.Note { return is.Notes }

// changesSince describes what changed since the user last read the issue.
func changesSince(is mantis.Issue, st *itemState, me string) []string {
	var out []string
	if st.SeenStatus != "" && st.SeenStatus != is.Status.Label {
		out = append(out, "狀態 "+st.SeenStatus+" → "+is.Status.Label)
	}
	newNotes := 0
	var lastAuthor string
	for _, n := range is.Notes {
		if n.Created.After(st.SeenUpdated) && !strings.EqualFold(n.Reporter.Name, me) {
			newNotes++
			lastAuthor = n.Reporter.Display()
		}
	}
	if newNotes > 0 {
		out = append(out, strconv.Itoa(newNotes)+" 則新留言（"+lastAuthor+"）")
	}
	if st.SeenTarget != is.TargetVersion && (st.SeenTarget != "" || is.TargetVersion != "") {
		t := is.TargetVersion
		if t == "" {
			t = "（無）"
		}
		out = append(out, "目標版本 → "+t)
	}
	if len(out) == 0 {
		out = append(out, "內容已更新")
	}
	return out
}

// onlyMyOwnChange reports whether the latest activity looks like the user's
// own (their note is the newest one and matches the update time).
func onlyMyOwnChange(is mantis.Issue, st *itemState, me string) bool {
	if len(is.Notes) == 0 || st.SeenStatus != is.Status.Label {
		return false
	}
	last := is.Notes[len(is.Notes)-1]
	if !strings.EqualFold(last.Reporter.Name, me) {
		return false
	}
	d := is.Updated.Sub(last.Created)
	return d >= -2*time.Minute && d <= 2*time.Minute
}

func buildView(base string, is mantis.Issue, st *itemState, me string) IssueView {
	v := IssueView{
		ID:             is.ID,
		URL:            base + "/view.php?id=" + strconv.Itoa(is.ID),
		Summary:        is.Summary,
		Project:        is.Project.Name,
		Category:       is.Category,
		Group:          statusGroup(is.Status),
		StatusLabel:    is.Status.Label,
		StatusColor:    is.Status.Color,
		Priority:       is.Priority.Label,
		Severity:       is.Severity.Label,
		Reporter:       is.Reporter.Display(),
		Handler:        is.Handler.Display(),
		TargetVersion:  is.TargetVersion,
		Created:        is.Created,
		Updated:        is.Updated,
		Description:    is.Description,
		Steps:          is.Steps,
		AdditionalInfo: is.AdditionalInfo,
		NoteCount:      len(is.Notes),
		Tags:           is.Tags,
	}
	if !is.DueDate.IsZero() {
		d := is.DueDate
		v.DueDate = &d
	}
	notes := visibleNotes(is)
	start := 0
	if len(notes) > 5 {
		start = len(notes) - 5
	}
	for _, n := range notes[start:] {
		v.Notes = append(v.Notes, NoteView{Author: n.Reporter.Display(), Text: n.Text, Created: n.Created, Private: n.Private})
	}
	if st != nil {
		if st.SeenUpdated.IsZero() {
			v.IsNew = true
		} else if is.Updated.After(st.SeenUpdated) {
			v.IsUpdated = true
			v.Changes = changesSince(is, st, me)
		}
	}
	return v
}

func sortViews(vs []IssueView) {
	sort.SliceStable(vs, func(i, j int) bool {
		if !vs[i].Updated.Equal(vs[j].Updated) {
			return vs[i].Updated.After(vs[j].Updated)
		}
		return vs[i].ID > vs[j].ID
	})
}
