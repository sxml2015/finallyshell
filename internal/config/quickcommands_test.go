package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// cmdIDs are the IDs of cmds, in order.
func cmdIDs(cmds []*QuickCommand) []string {
	var out []string
	for _, c := range cmds {
		out = append(out, c.ID)
	}
	return out
}

// No file is an empty list, and no error.
func TestQuickCommandsNoFile(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	q, err := LoadQuickCommands()
	if err != nil || len(q.Groups) != 0 || len(q.Commands) != 0 || q.Version != QuickCommandsVersion {
		t.Fatalf("%+v, %v", q, err)
	}
}

// The file of the issue reads as written; saving writes it back, through
// a temporary file, and reads the same.
func TestQuickCommandsRoundTrip(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	file := `{"version": 1,
 "groups":   [{"id": "g1", "name": "常用"}],
 "commands": [{"id": "c1", "group": "g1", "name": "更新系统",
               "text": "apt update\napt -y upgrade", "enter": true},
              {"id": "c2", "text": "tail -f /var/log/syslog", "enter": false},
              {"id": "c3", "name": "df", "text": "df -h"}]}`
	if err := os.WriteFile(filepath.Join(Dir(), "quickcommands.json"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	q, err := LoadQuickCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Groups) != 1 || q.Groups[0].Name != "常用" || !slices.Equal(cmdIDs(q.Commands), []string{"c1", "c2", "c3"}) {
		t.Fatalf("read %+v", q)
	}
	c1, c2, c3 := q.Command("c1"), q.Command("c2"), q.Command("c3")
	if c1.Group != "g1" || c1.Text != "apt update\napt -y upgrade" || !c1.Enter {
		t.Errorf("c1 %+v", c1)
	}
	if c2.Enter || c2.Title() != "tail -f /var/log/syslog" {
		t.Errorf("c2 %+v, title %q", c2, c2.Title())
	}
	if !c3.Enter {
		t.Error("a command without enter reads as false; the default is true")
	}
	if err := SaveQuickCommands(q); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(Dir())
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left: %s", e.Name())
		}
	}
	again, err := LoadQuickCommands()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cmdIDs(again.Commands), cmdIDs(q.Commands)) || !again.Command("c3").Enter || again.Command("c2").Enter {
		t.Errorf("read back %+v", again)
	}
	data, _ := os.ReadFile(filepath.Join(Dir(), "quickcommands.json"))
	if !strings.Contains(string(data), `"enter": true`) || !strings.Contains(string(data), `"enter": false`) {
		t.Errorf("enter not written out:\n%s", data)
	}
}

// A file that does not parse reads as an empty list, with an error
// naming the copy kept of it, which the next save leaves alone.
func TestQuickCommandsBrokenFile(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	at := time.Date(2026, 11, 9, 10, 30, 0, 0, time.Local)
	now = func() time.Time { return at }
	t.Cleanup(func() { now = time.Now })
	bad := []byte(`{"version": 1, "commands": [{"id": "c1", "text": "uptime"`)
	if err := os.WriteFile(filepath.Join(Dir(), "quickcommands.json"), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	q, err := LoadQuickCommands()
	var broken *BrokenFileError
	if !errors.As(err, &broken) || q == nil || len(q.Commands) != 0 {
		t.Fatalf("%+v, %v", q, err)
	}
	want := filepath.Join(Dir(), "quickcommands.json.broken-20261109-103000")
	if broken.Copy != want || !strings.Contains(err.Error(), want) {
		t.Errorf("copy %q, error %v", broken.Copy, err)
	}
	q.AddCommand(&QuickCommand{Text: "ls", Enter: true}, "")
	if err := SaveQuickCommands(q); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(want); string(data) != string(bad) {
		t.Errorf("the copy holds %q", data)
	}

	// An empty file, or null, is no list at all; nulls in the lists go,
	// and entries without IDs, or with one taken, get their own.
	for _, s := range []string{"", "  \n", "null"} {
		if q, err := ParseQuickCommands([]byte(s)); err != nil || len(q.Commands) != 0 {
			t.Errorf("%q: %+v, %v", s, q, err)
		}
	}
	q, err = ParseQuickCommands([]byte(`{"groups": [null, {"name": "a"}, {"id": "x", "name": "b"}],
		"commands": [null, {"text": "ls"}, {"id": "x", "text": "pwd"}, {"id": "y", "text": "id"}, {"id": "y", "text": "w"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, id := range append(cmdIDs(q.Commands), q.Groups[0].ID, q.Groups[1].ID) {
		if id == "" || seen[id] {
			t.Errorf("ID %q empty or twice", id)
		}
		seen[id] = true
	}
	if len(q.Groups) != 2 || len(q.Commands) != 4 || q.Version != QuickCommandsVersion {
		t.Errorf("parsed %+v", q)
	}
}

// Keys a newer build wrote stay, and so does its version.
func TestQuickCommandsUnknownKeys(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	file := `{"version": 3, "sync": {"at": 1}, "groups": [{"id": "g1", "name": "a", "color": "red"}],
		"commands": [{"id": "c1", "text": "ls", "enter": true, "shortcut": "F5"}]}`
	q, err := ParseQuickCommands([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveQuickCommands(q); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(Dir(), "quickcommands.json"))
	for _, want := range []string{`"version": 3`, `"sync"`, `"color": "red"`, `"shortcut": "F5"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("lost %s:\n%s", want, data)
		}
	}
}

func TestQuickCommandsEdits(t *testing.T) {
	q := NewQuickCommands()
	g := q.AddGroup("Docker")
	h := q.AddGroup("Logs")
	if g.ID == h.ID || q.Group(g.ID) != g || q.Group("") != nil {
		t.Fatalf("groups %+v", q.Groups)
	}
	add := func(name, group, after string) *QuickCommand {
		c := &QuickCommand{Name: name, Group: group, Text: name, Enter: true}
		q.AddCommand(c, after)
		return c
	}
	a := add("a", g.ID, "")
	b := add("b", "", "")
	c := add("c", h.ID, "")
	d := add("d", g.ID, a.ID) // right after a
	if got := cmdIDs(q.Commands); !slices.Equal(got, []string{a.ID, d.ID, b.ID, c.ID}) {
		t.Fatalf("order %v", got)
	}
	if got := cmdIDs(q.InGroup(g.ID)); !slices.Equal(got, []string{a.ID, d.ID}) {
		t.Errorf("group %v", got)
	}
	if len(q.InGroup("")) != 4 {
		t.Error("All is not every command")
	}

	// Dragging down puts it after the one dropped on, up before it, and
	// on nothing at the end.
	q.MoveCommand(a.ID, b.ID)
	if got := cmdIDs(q.Commands); !slices.Equal(got, []string{d.ID, b.ID, a.ID, c.ID}) {
		t.Errorf("down: %v", got)
	}
	q.MoveCommand(c.ID, d.ID)
	if got := cmdIDs(q.Commands); !slices.Equal(got, []string{c.ID, d.ID, b.ID, a.ID}) {
		t.Errorf("up: %v", got)
	}
	q.MoveCommand(c.ID, "")
	if got := cmdIDs(q.Commands); !slices.Equal(got, []string{d.ID, b.ID, a.ID, c.ID}) {
		t.Errorf("to the end: %v", got)
	}
	if q.MoveCommand(c.ID, c.ID) || q.MoveCommand(c.ID, "") || q.MoveCommand("nope", a.ID) || q.MoveCommand(a.ID, "nope") {
		t.Error("a move that changes nothing reported a change")
	}

	// A group removed leaves its commands in none.
	if !q.RemoveGroup(g.ID) || q.RemoveGroup(g.ID) || a.Group != "" || d.Group != "" || c.Group != h.ID {
		t.Errorf("after removing the group: %+v %+v %+v", a, d, c)
	}
	if !q.RemoveCommand(b.ID) || q.RemoveCommand(b.ID) || q.Command(b.ID) != nil || len(q.Commands) != 3 {
		t.Errorf("after removing b: %v", cmdIDs(q.Commands))
	}
}

// Merging adds the groups and commands of the other, keeping what is
// here: a group joins the one of the same name, a command identical to
// one already in its group is skipped, and what is added gets new IDs,
// each command in the group it came with.
func TestQuickCommandsMerge(t *testing.T) {
	q := NewQuickCommands()
	common := q.AddGroup("Common")
	q.AddCommand(&QuickCommand{Name: "Disk", Group: common.ID, Text: "df -h", Enter: true}, "")
	other := &QuickCommands{
		Version: QuickCommandsVersion,
		Groups: []*QuickGroup{
			{ID: "g1", Name: "Common"},
			{ID: "g2", Name: "Docker"},
		},
		Commands: []*QuickCommand{
			{ID: "c1", Group: "g1", Name: "Disk", Text: "df -h", Enter: true},
			{ID: "c2", Group: "g1", Name: "Memory", Text: "free -m", Enter: true},
			{ID: "c3", Group: "g2", Name: "List", Text: "docker ps", Enter: true},
			{ID: "c4", Name: "Uptime", Text: "uptime", Enter: true},
			{ID: "c5", Group: "gone", Name: "Orphan", Text: "id", Enter: true},
			{ID: "c6", Group: "g1", Name: "Disk", Text: "df -h", Enter: false},
		},
	}
	groups, added, skipped := q.MergeQuickCommands(other)
	if groups != 1 || added != 5 || skipped != 1 {
		t.Fatalf("merged %d groups, %d commands, %d skipped", groups, added, skipped)
	}
	if len(q.Groups) != 2 {
		t.Fatalf("groups %+v", q.Groups)
	}
	docker := q.Groups[1]
	if docker.Name != "Docker" || docker.ID == "g2" {
		t.Errorf("new group %+v", docker)
	}
	byName := func(name string) *QuickCommand {
		for _, c := range q.Commands {
			if c.Name == name {
				return c
			}
		}
		return nil
	}
	if c := byName("Memory"); c == nil || c.Group != common.ID {
		t.Errorf("Memory in %+v", c)
	}
	if c := byName("List"); c == nil || c.Group != docker.ID {
		t.Errorf("List in %+v", c)
	}
	for _, name := range []string{"Uptime", "Orphan"} {
		if c := byName(name); c == nil || c.Group != "" {
			t.Errorf("%s in %+v", name, c)
		}
	}
	// Nothing added keeps an ID of the other's: every one is new.
	others := map[string]bool{}
	for _, c := range other.Commands {
		others[c.ID] = true
	}
	for _, id := range cmdIDs(q.Commands) {
		if others[id] {
			t.Errorf("kept the other's ID %q", id)
		}
	}
	disks := 0
	for _, c := range q.InGroup(common.ID) {
		if c.Name == "Disk" {
			disks++
		}
	}
	if disks != 2 {
		t.Errorf("Common has %d Disks", disks)
	}
	// Merging the same list again adds nothing: every one is identical
	// now, and the groups are all here.
	if groups, added, skipped := q.MergeQuickCommands(other); groups != 0 || added != 0 || skipped != 6 {
		t.Errorf("again: %d groups, %d commands, %d skipped", groups, added, skipped)
	}
	if groups, added, skipped := q.MergeQuickCommands(nil); groups != 0 || added != 0 || skipped != 0 {
		t.Errorf("nil: %d groups, %d commands, %d skipped", groups, added, skipped)
	}
}

func TestQuickCommandLines(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"", nil},
		{"  \n\t\n", nil},
		{"df -h", []string{"df -h"}},
		{"df -h\n", []string{"df -h"}},
		{"apt update\r\napt -y upgrade\r\n\r\n", []string{"apt update", "apt -y upgrade"}},
		{"cat <<EOF\n\nhi\nEOF", []string{"cat <<EOF", "", "hi", "EOF"}},
		{"  indented  ", []string{"  indented  "}},
	} {
		if got := QuickCommandLines(tc.text); !slices.Equal(got, tc.want) {
			t.Errorf("%q: %q, want %q", tc.text, got, tc.want)
		}
	}
	for _, tc := range []struct{ name, text, want string }{
		{"Disk", "df -h", "Disk"},
		{"  ", "\n  apt update\napt upgrade", "apt update"},
		{"", "", ""},
	} {
		c := &QuickCommand{Name: tc.name, Text: tc.text}
		if got := c.Title(); got != tc.want {
			t.Errorf("%q %q: title %q, want %q", tc.name, tc.text, got, tc.want)
		}
	}
}
