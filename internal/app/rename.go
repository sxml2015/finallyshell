package app

import (
	"slices"
	"strings"
	"unicode"

	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// A session tab can have a name of its own for this run (Session.name),
// which its tab and everything that names the tab show instead of the
// host's title: the user renames it in place, as a file in Finder or
// Explorer, from the tab's context menu or with F2 in the tab strip.
// The name is never saved; closing the tab forgets it.

// renameState is the session tab being renamed: its session, nil when
// none, and the text of its field.
type renameState struct {
	s    *Session
	text string
	// fresh is set until the field has shown once: it then takes the
	// focus, with its text selected.
	fresh bool
}

// canRename reports whether s's tab can be renamed: a new tab has no host
// to name it after yet, and a panel tab (paneltab.go) is named after its
// session and its tab.
func canRename(s *Session) bool { return !s.isNewTab() && !s.isPanelTab() }

// startRename shows the field in place of the name of s's tab.
func (a *App) startRename(s *Session) {
	if !canRename(s) || a.rename.s == s {
		return
	}
	a.rename = renameState{s: s, text: s.label(), fresh: true}
}

// endRename hides the field, giving its session the name typed when keep
// is set.
func (a *App) endRename(keep bool) {
	if keep && a.rename.s != nil {
		a.rename.s.setName(a.rename.text)
	}
	a.rename = renameState{}
}

// renaming reports whether s's tab shows the field. A session closed while
// it did shows none again.
func (a *App) renaming(s *Session) bool {
	if a.rename.s != nil && !slices.Contains(a.sessions, a.rename.s) {
		a.rename = renameState{}
	}
	return a.rename.s == s && s != nil
}

// setName names s's tab name for this run, trimmed and on one line: an
// empty name, or the host's title ("Local terminal" for a local
// terminal), gives it back the one it had.
func (s *Session) setName(name string) {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name))
	if s.Host != nil && name == s.Host.Title() || s.isLocal() && name == locales.LocalTerminal() {
		name = ""
	}
	s.name = name
}

// renameMenu is the items of a session tab's context menu that rename it.
func (a *App) renameMenu(m *ui.Menu, s *Session) {
	if !canRename(s) {
		return
	}
	if m.Item(locales.Rename()).Shortcut(0, ui.KeyF2).Chosen() {
		a.startRename(s)
	}
	if s.name != "" && m.Item(locales.ResetName()).Chosen() {
		a.endRename(false)
		s.name = ""
	}
}

// renameKey renames the selected session with F2 while the tab strip
// has the keyboard focus. The terminal keeps F2 for its programs.
func (a *App) renameKey(strip ui.Element) {
	if strip.Shortcut(0, ui.KeyF2) && a.rename.s == nil {
		if s := a.current(); s != nil {
			a.startRename(s)
		}
	}
}

// renamedTooltip is the tooltip of the tab of a session with a name of
// its own: the host it is a session of, or that it is a local terminal.
func renamedTooltip(s *Session) string {
	if s.isLocal() {
		return locales.LocalTerminal()
	}
	return s.Host.Title() + "\n" + s.Host.Target()
}

// renameField is the field in place of the name of the tab being
// renamed: Enter keeps what was typed, and Escape, a click elsewhere or
// the focus moving away goes back to the name it had. It takes the keys
// the tabs would take (Up, Down, Page Up, Page Down); a press in it
// selects text rather than dragging the tab.
func (a *App) renameField(c *ui.Context, tab ui.Element, active bool) {
	r := &a.rename
	pad := float32(2)
	in := ui.TextInputBase(c, &r.text).FillWidth().MinWidth(0).
		Padding(pad, pad*2).Margin(-pad-1, -pad*2-1).Radius(4).
		Background(theme.Background).Border(1, theme.Accent).TextColor(theme.Text).
		Label(locales.Rename())
	if active {
		in.Bold()
	}
	for _, k := range []ui.Key{ui.KeyUp, ui.KeyDown, ui.KeyPageUp, ui.KeyPageDown} {
		in.Shortcut(0, k)
	}
	switch {
	case r.fresh:
		r.fresh = false
		in.Focus().SetTextSelection(0, len([]rune(r.text)))
	case in.Submitted():
		a.endRename(true)
		tab.Focus() // the keys go to the tab again, as in Finder
	case in.Shortcut(0, ui.KeyEscape):
		a.endRename(false)
		tab.Focus()
	case in.PressedOutside():
		a.endRename(false)
	case in.FocusLeft() && !in.FocusWithin():
		// The focus went elsewhere in the window. The window losing the
		// keyboard, as the user switches apps, leaves it on the field
		// (FocusWithin, unlike FocusLeft, does not ask the window), which
		// keeps it.
		a.endRename(false)
	}
}
