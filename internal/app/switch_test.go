package app

import (
	"image/color"
	"slices"
	"testing"

	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// switchApp opens the tabs of steps in order, each selected as it opens,
// as the app selects a new session or a new tab: a session name for a
// connected session with a colored terminal (coloredSession), "+" for a
// new tab. It returns the color each session's terminal shows.
func switchApp(t *testing.T, steps []string) (*App, *ui.Tester, map[*Session]color.RGBA) {
	t.Helper()
	a, p := testApp(t)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	t.Cleanup(a.CloseAll)
	shows := map[*Session]color.RGBA{}
	for _, step := range steps {
		if step == "+" {
			a.NewTab()
		} else {
			coloredSession(t, a, step, len(shows))
			a.active = len(a.sessions) - 1
		}
		tt.Frame()
		if s := a.current(); !s.isNewTab() {
			shows[s] = tt.Image().RGBAAt(termSpotX, termSpotY)
		}
	}
	for s, c := range shows {
		for o, d := range shows {
			if s != o && c == d {
				t.Fatalf("the terminals of %s and %s look alike: %v", s.label(), o.label(), c)
			}
		}
	}
	return a, tt, shows
}

// tabCenters returns the middle of each tab of the session tab strip.
// The tabs of named sessions are found by their labels; a new tab's is
// the middle of its place in the row: every new tab is as wide as the
// first of them, and the tabs run from the strip's left padding, 2
// apart.
func tabCenters(t *testing.T, a *App, tt *ui.Tester) [][2]float32 {
	t.Helper()
	centers := make([][2]float32, len(a.sessions))
	// The width of a new tab, from the first of them.
	newW := float32(0)
	if r, ok := tt.AccessByRole(ui.RoleTab, locales.NewTab()); ok {
		newW = r.Bounds.W
	}
	// The tabs' row, from a named tab, or the strip's new tab button.
	y := float32(0)
	for _, s := range a.sessions {
		if !s.isNewTab() {
			r, ok := tt.AccessByRole(ui.RoleTab, s.label())
			if !ok {
				t.Fatalf("no tab %q; texts %q", s.label(), tt.Texts())
			}
			y = r.Bounds.Y + r.Bounds.H/2
			break
		}
	}
	plus, ok := tt.AccessByRole(ui.RoleButton, locales.NewTab())
	if !ok {
		t.Fatal("no new tab button")
	}
	if y == 0 {
		y = plus.Bounds.Y + plus.Bounds.H/2
	}
	// The tabs start where the first of them is, 2 apart.
	edge := float32(0)
	if s := a.sessions[0]; s.isNewTab() {
		if r, ok := tt.AccessByRole(ui.RoleTab, locales.NewTab()); ok {
			edge = r.Bounds.X
		}
	} else if r, ok := tt.AccessByRole(ui.RoleTab, s.label()); ok {
		edge = r.Bounds.X
	}
	for i, s := range a.sessions {
		if !s.isNewTab() {
			r, _ := tt.AccessByRole(ui.RoleTab, s.label())
			centers[i] = [2]float32{r.Bounds.X + r.Bounds.W/2, y}
			edge = r.Bounds.X + r.Bounds.W + 2
			continue
		}
		if newW < 20 {
			t.Fatalf("new tabs %.1f wide", newW)
		}
		centers[i] = [2]float32{edge + newW/2, y}
		edge += newW + 2
	}
	// The first new tab's middle, as found, against its place in the
	// row.
	for i, s := range a.sessions {
		if s.isNewTab() {
			if r, ok := tt.AccessByRole(ui.RoleTab, locales.NewTab()); ok {
				if got := r.Bounds.X + r.Bounds.W/2; got != centers[i][0] {
					t.Fatalf("new tab %d at %.1f, not %.1f", i, got, centers[i][0])
				}
			}
			break
		}
	}
	return centers
}

// checkShows fails unless tab i is selected and its terminal shows, or
// the start page for a new tab, in this frame and the next.
func checkShows(t *testing.T, a *App, tt *ui.Tester, shows map[*Session]color.RGBA, i int, how string) {
	t.Helper()
	want := a.sessions[i]
	for _, when := range []string{"", " a frame later"} {
		if got := a.current(); got != want {
			t.Fatalf("%s tab %d (%s): tab %d selected%s", how, i, want.label(), a.active, when)
		}
		px := tt.Image().RGBAAt(termSpotX, termSpotY)
		start := tt.HasText(locales.QuickConnect())
		if want.isNewTab() {
			if !start {
				t.Errorf("%s tab %d (new tab): no start page%s", how, i, when)
			}
			for s, c := range shows {
				if px == c {
					t.Errorf("%s tab %d (new tab): %s's terminal shows%s", how, i, s.label(), when)
				}
			}
		} else {
			if start {
				t.Errorf("%s tab %d (%s): the start page shows%s", how, i, want.label(), when)
			}
			if px != shows[want] {
				t.Errorf("%s tab %d (%s): the terminal shows %v, want %v%s", how, i, want.label(), px, shows[want], when)
			}
		}
		tt.Frame()
	}
}

// TestClickSessionTabSelectsIt clicks every tab of the session tab strip,
// whatever order its sessions and new tabs opened in, the older ones
// first and back: each click selects the tab and shows its terminal, or
// its start page (#38). The keys that switch tabs keep working in
// between.
func TestClickSessionTabSelectsIt(t *testing.T) {
	cases := []struct {
		name  string
		steps []string
	}{
		{"sessions only", []string{"alpha", "bravo", "charlie"}},
		{"sessions then a new tab", []string{"alpha", "bravo", "+"}},
		{"a new tab first", []string{"+", "alpha", "bravo"}},
		{"interleaved", []string{"alpha", "+", "bravo", "+"}},
	}
	for _, k := range cases {
		t.Run(k.name, func(t *testing.T) {
			a, tt, shows := switchApp(t, k.steps)
			n := len(a.sessions)
			if a.active != n-1 {
				t.Fatalf("the last tab opened is not selected (active %d)", a.active)
			}
			centers := tabCenters(t, a, tt)
			order := []int{}
			for i := range n {
				order = append(order, i)
			}
			for i := n - 2; i >= 0; i-- {
				order = append(order, i)
			}
			for _, i := range order {
				tt.ClickAt(centers[i][0], centers[i][1])
				checkShows(t, a, tt, shows, i, "click")
			}
			if k.name == "interleaved" {
				saveShot(t, tt, "click-session-tab.png")
			}

			// Tab 0 is selected: the keys move from it, and a click after
			// them still selects.
			next, prev, digit := a.shortcut(scNextTab), a.shortcut(scPreviousTab), a.shortcut(scSwitchTab)
			tt.Key(next.Mods, next.Key)
			checkShows(t, a, tt, shows, 1, "next tab to")
			tt.Key(prev.Mods, prev.Key)
			checkShows(t, a, tt, shows, 0, "previous tab to")
			tt.Key(prev.Mods, prev.Key)
			checkShows(t, a, tt, shows, n-1, "previous tab around to")
			tt.Key(digit.Mods, digitKeys[1])
			checkShows(t, a, tt, shows, 1, "switch tab to")
			tt.ClickAt(centers[n-1][0], centers[n-1][1])
			checkShows(t, a, tt, shows, n-1, "click after the keys")
			tt.ClickAt(centers[0][0], centers[0][1])
			checkShows(t, a, tt, shows, 0, "click after the keys")
		})
	}
}

// TestDoubleClickTabReconnects double-clicks a disconnected session's
// tab, which reconnects it: a single click only selects it, and a double
// click on a connected session's tab leaves it connected.
func TestDoubleClickTabReconnects(t *testing.T) {
	a, tt, _ := switchApp(t, []string{"alpha", "bravo", "+"})
	alpha, bravo := a.sessions[0], a.sessions[1]
	bravo.status = statusClosed
	// Refused at once: the reconnect only has to start.
	bravo.Host.Addr, bravo.Host.Port = "127.0.0.1", 1
	a.passwords[bravo.Host.ID] = "secret"
	tt.Frame()
	centers := tabCenters(t, a, tt)

	tt.ClickAt(centers[1][0], centers[1][1])
	tt.Frame()
	if a.current() != bravo || bravo.status != statusClosed {
		t.Fatalf("a click: tab %d selected, bravo %v", a.active, bravo.status)
	}
	tt.ClickAt(centers[0][0], centers[0][1])
	tt.ClickAt(centers[0][0], centers[0][1])
	tt.Frame()
	if a.current() != alpha || alpha.status != statusConnected {
		t.Fatalf("a double click on alpha: tab %d selected, alpha %v", a.active, alpha.status)
	}
	tt.ClickAt(centers[1][0], centers[1][1])
	tt.ClickAt(centers[1][0], centers[1][1])
	tt.Frame()
	if a.current() != bravo || bravo.status != statusConnecting {
		t.Fatalf("a double click on bravo: tab %d selected, bravo %v", a.active, bravo.status)
	}
	if !slices.Contains(a.sessions, bravo) || len(a.sessions) != 3 {
		t.Errorf("reconnecting changed the tabs: %d", len(a.sessions))
	}
}
