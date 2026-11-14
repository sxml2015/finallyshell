package app

import (
	"slices"

	"finallyshell/internal/icons"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The session tabs are the row above the terminal, as FinalShell's: one
// tab per session, a panel row's among them, with a local terminal and a
// new tab at its right end. A tab shows its session's status mark and
// name, and a close button on the tab chosen and the one under the
// pointer; a click chooses it, a double click on one not connected
// connects it again, and its context menu is the session's. Tabs drag to
// another place in the row, and scroll sideways where they do not fit.
// The strip scrolls to a tab chosen from elsewhere, as with the keys, so
// that it shows; scrolling it by hand leaves it where it is.

// sessionTabMaxWidth is the widest a tab gets, in DIPs: a long name is
// cut short, and the tooltip says where the session goes.
const sessionTabMaxWidth = 200

// sessionTabDrag is the value a tab drags: the session it moves.
type sessionTabDrag struct{ s *Session }

// sessionCount is the number of sessions, local terminals included, new
// tabs and panel rows left out.
func (a *App) sessionCount() int {
	n := 0
	for _, s := range a.sessions {
		if !s.isNewTab() {
			n++
		}
	}
	return n
}

// sessionTabStrip is the row of tabs above the terminal, with the local
// terminal and the new tab at its right end.
func (a *App) sessionTabStrip(c *ui.Context) {
	strip := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(2).Padding(0, 4, 0, 0).
		Background(colors.sidebar).BorderWidth(0, 0, 1, 0).BorderColor(colors.line)
	// A tab asks to close its session, or to move it, which happens once
	// the strip is built: closing or moving it in the tab would leave
	// the tabs after it to build with fewer sessions than the strip was
	// given, or in another order.
	var closing *Session
	var move *sessionTabDrag
	moveTo := -1
	var activeBounds ui.Rect
	strip.Children(func() {
		scroll := ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Scrollbars(false).
			TrackScroll(&a.sessionTabsScroll).HandleInput(a.sessionTabsWheel)
		scroll.Children(func() {
			if len(a.sessions) == 0 {
				widgets.Muted(c, locales.NoSessions()).Padding(8, 10)
				return
			}
			tabs := ui.TabsBase(c, &a.active, len(a.sessions))
			tabs.List.Gap(2).Padding(4, 4, 0, 4).Children(func() {
				for i, s := range a.sessions {
					tab := tabs.Tab(i)
					if i == a.active {
						activeBounds = tab.Bounds()
					}
					if a.sessionTab(c, tab, s, i, i == a.active, &move, &moveTo) {
						closing = s
					}
				}
			})
			// The tab chosen from elsewhere, as with the keys, is
			// scrolled into view once it has a place to scroll to.
			if cur := a.current(); cur != nil && cur.ID != a.scrolledTab {
				if a.scrollTabIntoView(scroll, activeBounds) {
					a.scrolledTab = cur.ID
				}
			}
		})
		if headerButton(c, "square-terminal", locales.LocalTerminal()).Clicked() {
			a.openLocal(nil)
		}
		if headerButton(c, "plus", locales.NewTab()).Clicked() {
			a.NewTab()
		}
	})
	// A tab dropped past the tabs, on the strip itself, goes at the end.
	if d, ok := ui.Drop[sessionTabDrag](strip); ok && move == nil {
		move, moveTo = &d, len(a.sessions)
	}
	a.renameKey(strip)
	if move != nil && moveTo >= 0 {
		if i := slices.Index(a.sessions, move.s); i >= 0 {
			a.moveSessions([]int{i}, moveTo)
		}
	}
	if closing != nil {
		a.closeSession(closing)
	}
}

// sessionTab is the tab of s, the i-th: its status mark, name and close
// button. It reports whether the user closed it, and takes a tab dropped
// on it for the move the strip makes once built.
func (a *App) sessionTab(c *ui.Context, tab ui.Element, s *Session, i int, active bool, move **sessionTabDrag, moveTo *int) (closed bool) {
	if s.isPanelTab() {
		return a.panelTab(c, tab, s, i, active, move, moveTo)
	}
	renaming := a.renaming(s)
	tab.Label(s.label()).MaxWidth(sessionTabMaxWidth)
	tabStyle(tab, active)
	if tab.Dragging() {
		tab.Opacity(0.4) // where the tab was, as it moves with the pointer
	}
	tabDrop(tab, i, move, moveTo)
	if s.isNewTab() {
		tab.ContextMenu(func(m *ui.Menu) {
			if m.Item(locales.Close()).Danger().Chosen() {
				closed = true
			}
		})
		tab.Drag(sessionTabDrag{s})
		tab.Children(func() {
			// As wide as a status dot, so that the names line up.
			ui.Box(c).Size(8, 8).Center().Shrink(0).Children(func() {
				ui.Icon(c, icons.Get("app-window")).FontSize(13).TextColor(theme.TextMuted)
			})
			rowName(c, locales.NewTab(), active).MinWidth(0).Shrink(1)
			closed = tabCloseButton(c, tab, locales.NewTab(), active) || closed
		})
		return closed
	}
	tab.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.Reconnect()).Chosen() {
			a.reconnect(s)
		}
		if m.Item(locales.DuplicateSession()).Chosen() {
			// In a tab of its own, though a new tab is selected.
			if s.isLocal() {
				a.openLocal(nil)
			} else {
				a.openSessionIn(nil, s.Host)
			}
		}
		if m.Item(locales.Disconnect()).Disabled(s.status != statusConnected && s.status != statusConnecting).Chosen() {
			a.transfers.cancelSession(s)
			s.disconnect()
			s.status, s.err = statusClosed, nil
		}
		m.Separator()
		a.paneMenu(m, s)
		m.Separator()
		a.renameMenu(m, s)
		m.Separator()
		if m.Item(locales.Close()).Danger().Chosen() {
			closed = true
		}
	})
	if tab.DoubleClicked() && !renaming && s.status != statusConnected && s.status != statusConnecting {
		a.reconnect(s)
	}
	sub := s.target()
	switch {
	case s.status != statusConnected:
		sub = s.statusText()
	case s.isLocal() && s.programTitle() != "":
		sub = s.programTitle() // the program's own, as a terminal app's tab
	}
	if s.name != "" && !renaming {
		tab.Tooltip(renamedTooltip(s))
	} else {
		tab.Tooltip(sub)
	}
	tab.Drag(sessionTabDrag{s})
	tab.Children(func() {
		sessionMark(c, s, active)
		if renaming {
			a.renameField(c, tab, active)
		} else {
			a.sessionName(c, s, active)
		}
		closed = tabCloseButton(c, tab, s.label(), active) || closed
	})
	return closed
}

// panelTab is the tab of the panel row p: its tab's icon and name, as
// the tab of its session's panel. It does not drag: it moves with its
// session, and a tab dropped on it goes below it (moveSessions).
func (a *App) panelTab(c *ui.Context, tab ui.Element, p *Session, i int, active bool, move **sessionTabDrag, moveTo *int) (closed bool) {
	tab.Label(panelLabel(p)).MaxWidth(sessionTabMaxWidth).Tooltip(panelLabel(p))
	tabStyle(tab, active)
	tabDrop(tab, i, move, moveTo)
	tab.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.BackToTerminal()).Chosen() {
			a.backToTerminal(p)
		}
		m.Separator()
		if m.Item(locales.Close()).Danger().Chosen() {
			closed = true
		}
	})
	tab.Children(func() {
		icon := "app-window"
		if p.panelTab >= 0 && p.panelTab < len(panelTabIcons) {
			icon = panelTabIcons[p.panelTab]
		}
		ui.Icon(c, icons.Get(icon)).FontSize(14).Shrink(0)
		name := ui.Text(c, panelTabName(p)).SingleLine().MinWidth(0).Shrink(1)
		if active {
			name.Bold()
		}
		closed = tabCloseButton(c, tab, panelTabName(p), active) || closed
	})
	return closed
}

// tabStyle is the look of a tab: the chosen one raised, as a folder's
// tab, over the strip, and the one under the pointer shaded.
func tabStyle(tab ui.Element, active bool) {
	tab.Gap(6).AlignItems(ui.Center).Padding(6, 4, 6, 10).Radius(6, 6, 0, 0)
	switch {
	case active:
		tab.Background(theme.Background)
	case tab.Hovered():
		tab.Background(theme.Text.Alpha(0.06))
	}
}

// tabCloseButton is the close button at the tab's right end, which shows
// on the chosen tab and the one under the pointer. It reports whether it
// was clicked.
func tabCloseButton(c *ui.Context, tab ui.Element, name string, active bool) bool {
	closeBtn := widgets.IconButton(c, "x", locales.CloseTab(name)).Size(20, 20)
	if !tab.Hovered() && !active {
		closeBtn.Invisible()
	}
	return closeBtn.Clicked()
}

// tabDrop takes a tab dropped on the i-th tab, for the move the strip
// makes once built: before it when the pointer let go on its left half,
// after it on its right half. While a tab is dragged over it, an accent
// line marks where it would go.
func tabDrop(tab ui.Element, i int, move **sessionTabDrag, moveTo *int) {
	if d, ok := ui.Drop[sessionTabDrag](tab); ok {
		*move = &d
		*moveTo = i + 1
		if x, _, _ := tab.PointerPosition(); x < tab.Bounds().W/2 {
			*moveTo = i
		}
		return
	}
	if _, ok := ui.DragOver[sessionTabDrag](tab); ok {
		x, _, over := tab.PointerPosition()
		if !over {
			return
		}
		left := x < tab.Bounds().W/2
		tab.DrawOver(func(p *ui.Painter, r ui.Rect) {
			edge := r.X + r.W - 2
			if left {
				edge = r.X
			}
			p.Fill(ui.Rect{X: edge, Y: r.Y + 2, W: 2, H: r.H - 4}, theme.Accent, 1)
		})
	}
}

// scrollTabIntoView scrolls the strip so that the tab at r, as the last
// frame laid it out, shows: false while it has no place yet, to try
// again in the next frame.
func (a *App) scrollTabIntoView(scroll ui.Element, r ui.Rect) bool {
	if r.W == 0 {
		return false
	}
	v := scroll.Bounds()
	if v.W == 0 {
		return false
	}
	s := &a.sessionTabsScroll
	switch {
	case r.X < v.X:
		s.X = max(s.X-(v.X-r.X), 0)
	case r.X+r.W > v.X+v.W:
		s.X = min(s.X+(r.X+r.W-(v.X+v.W)), s.MaxX)
	}
	return true
}

// sessionTabsWheel has the vertical wheel, or a vertical swipe of a
// touchpad, over the tabs scroll them sideways while they do not fit,
// as the bottom panel's tabs do (panelhide.go).
func (a *App) sessionTabsWheel(ev ui.InputEvent) bool {
	return scrollTabsSideways(&a.sessionTabsScroll, ev)
}
