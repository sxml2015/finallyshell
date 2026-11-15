package app

import (
	"testing"

	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// tabPixel is the color of the tester's image at the bottom padding of
// the tab named name: its background, below its text.
func tabPixel(t *testing.T, tt *ui.Tester, name string) (r, g, b uint8) {
	t.Helper()
	tab, ok := tt.AccessByRole(ui.RoleTab, name)
	if !ok {
		t.Fatalf("no tab %q; texts %q", name, tt.Texts())
	}
	px := tt.Image().RGBAAt(int(tab.Bounds.X+tab.Bounds.W/2), int(tab.Bounds.Y+tab.Bounds.H-3))
	return px.R, px.G, px.B
}

// isColor is whether r, g, b are the color c.
func isColor(r, g, b uint8, c ui.Color) bool { return r == c.R && g == c.G && b == c.B }

// The tab of the session shown is the accent color, so that it stands
// out from the rest at a glance; switching tabs moves the accent with
// the session.
func TestActiveTabStandsOut(t *testing.T) {
	a, p := testApp(t)
	demoSession(a, "web-1", "45.56.78.90")
	demoSession(a, "db-1", "45.56.78.91")
	a.active = 0
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()

	if r, g, b := tabPixel(t, tt, "web-1"); !isColor(r, g, b, theme.Accent) {
		t.Errorf("active tab: rgb(%d,%d,%d), not the accent %v", r, g, b, theme.Accent)
	}
	if r, g, b := tabPixel(t, tt, "db-1"); isColor(r, g, b, theme.Accent) {
		t.Errorf("inactive tab is the accent too")
	}

	a.active = 1
	tt.Frame()
	if r, g, b := tabPixel(t, tt, "db-1"); !isColor(r, g, b, theme.Accent) {
		t.Errorf("after switching: rgb(%d,%d,%d), not the accent %v", r, g, b, theme.Accent)
	}
	if r, g, b := tabPixel(t, tt, "web-1"); isColor(r, g, b, theme.Accent) {
		t.Errorf("after switching the old tab is still the accent")
	}
}

// The new tab sits right after the last tab, a short move from the
// tabs, and not at the strip's right end with the local terminal.
func TestNewTabButtonAfterLastTab(t *testing.T) {
	a, p := testApp(t)
	demoSession(a, "web-1", "45.56.78.90")
	demoSession(a, "db-1", "45.56.78.91")
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()

	plus, ok := tt.Access(locales.NewTab())
	if !ok {
		t.Fatalf("no new tab button; texts %q", tt.Texts())
	}
	last, ok := tt.AccessByRole(ui.RoleTab, "db-1")
	if !ok {
		t.Fatalf("no tab db-1; texts %q", tt.Texts())
	}
	right := last.Bounds.X + last.Bounds.W
	if plus.Bounds.X < right || plus.Bounds.X > right+12 {
		t.Errorf("new tab at x %.0f, last tab ends at %.0f", plus.Bounds.X, right)
	}
	term, ok := tt.Access(locales.LocalTerminal())
	if !ok {
		t.Fatalf("no local terminal button; texts %q", tt.Texts())
	}
	if plus.Bounds.X+plus.Bounds.W > term.Bounds.X-50 {
		t.Errorf("new tab ends at %.0f, local terminal starts at %.0f", plus.Bounds.X+plus.Bounds.W, term.Bounds.X)
	}

	// With no sessions at all, the new tab is still there, in the
	// strip at the top.
	b, q := testApp(t)
	tt = ui.NewTester(b.View, 1280, 820)
	q.tt = tt
	tt.Frame()
	plus, ok = tt.Access(locales.NewTab())
	if !ok {
		t.Fatalf("no new tab button without sessions; texts %q", tt.Texts())
	}
	if plus.Bounds.Y > 60 {
		t.Errorf("new tab button at y %.0f, not in the strip", plus.Bounds.Y)
	}
}
