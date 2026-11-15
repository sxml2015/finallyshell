package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"finallyshell/internal/remotefs"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// fileOp is what a file dialog does on the server.
type fileOp int

const (
	opRename fileOp = iota
	opMkdir
	opCreate
	opDelete
	opChmod
)

// fileDialog asks for the name of a rename, a new folder or a new file,
// the permissions of a file or folder, or whether to delete, in the
// file panel of sess, and shows how it went.
type fileDialog struct {
	sess  *Session
	op    fileOp
	dir   string   // the folder it happens in
	old   string   // the name renamed, or whose permissions change
	isDir bool     // what is renamed is a folder
	names []string // the names in dir a delete removes
	dirs  bool     // a delete removes folders, with what they hold
	name  string
	// selectStem selects the name without its extension as the dialog
	// opens, as Finder and Explorer do for a rename.
	selectStem bool
	// mode are the permissions opChmod sets, the set-ID and sticky bits
	// included; octal is its field's text, and octalBad that the text
	// does not parse.
	mode     fs.FileMode
	octal    string
	octalBad bool

	busy   bool
	err    error
	cancel context.CancelFunc
}

func (d *fileDialog) title() string {
	switch d.op {
	case opRename:
		return locales.RenameFileTitle(d.old)
	case opMkdir:
		return locales.NewFolder()
	case opCreate:
		return locales.NewFile()
	case opChmod:
		return locales.PermissionsTitle(d.old)
	}
	if len(d.names) == 1 {
		return locales.DeleteFileTitle(d.names[0])
	}
	return locales.DeleteFilesTitle(len(d.names))
}

// askRename asks for a new name of the file or folder at p.
func (a *App) askRename(s *Session, p string, isDir bool) {
	if s.sftp == nil || p == "/" || p == "" {
		return
	}
	name := path.Base(p)
	a.fileDlg = &fileDialog{sess: s, op: opRename, dir: path.Dir(p), old: name, isDir: isDir, name: name, selectStem: true}
}

// askNew asks for the name of a new folder or file in dir.
func (a *App) askNew(s *Session, dir string, op fileOp) {
	if s.sftp == nil || dir == "" {
		return
	}
	a.fileDlg = &fileDialog{sess: s, op: op, dir: dir}
}

// askDelete asks before deleting the names in dir; dirs tells that some
// are folders.
func (a *App) askDelete(s *Session, dir string, names []string, dirs bool) {
	if s.sftp == nil || dir == "" || len(names) == 0 {
		return
	}
	a.fileDlg = &fileDialog{sess: s, op: opDelete, dir: dir, names: names, dirs: dirs}
}

// askDeleteChosen asks before deleting the rows chosen in the table.
func (a *App) askDeleteChosen(s *Session) {
	f := &s.files
	names := f.chosenNames()
	dirs := false
	for _, e := range f.entries {
		// A link to a folder goes alone, as a file does.
		if f.chosen.Has(e.Name) && e.IsDir() && !e.Link {
			dirs = true
		}
	}
	a.askDelete(s, f.cwd, names, dirs)
}

// askPermissions asks for the permissions of the file or folder at p,
// whose mode is its current one.
func (a *App) askPermissions(s *Session, p string, mode fs.FileMode) {
	if s.sftp == nil || p == "/" || p == "" {
		return
	}
	// The type bits are not the file's to change, and a link's mode is
	// its target's.
	m := mode & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
	a.fileDlg = &fileDialog{sess: s, op: opChmod, dir: path.Dir(p), old: path.Base(p), mode: m, octal: modeOctal(m)}
}

func (a *App) closeFileDialog(d *fileDialog) {
	if d.cancel != nil {
		d.cancel()
	}
	if a.fileDlg == d {
		a.fileDlg = nil
	}
}

// runFileDialog does what d asks on the server, in a goroutine. The
// dialog closes when it is done, or shows the error.
func (a *App) runFileDialog(d *fileDialog) {
	sc := d.sess.sftp
	if sc == nil || d.busy {
		return
	}
	name := strings.TrimSpace(d.name)
	if d.op != opDelete && d.op != opChmod && !remotefs.ValidName(name) {
		d.err = remotefs.ErrBadName
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.busy, d.err, d.cancel = true, nil, cancel
	go func() {
		var made string   // the new path of a rename, a folder or a file
		var gone []string // the paths a delete removed
		var err error
		switch d.op {
		case opRename:
			made, err = remotefs.Rename(sc, path.Join(d.dir, d.old), name)
		case opMkdir:
			made, err = remotefs.Mkdir(sc, d.dir, name)
		case opCreate:
			made, err = remotefs.CreateFile(sc, d.dir, name)
		case opChmod:
			err = sc.Chmod(path.Join(d.dir, d.old), d.mode)
		case opDelete:
			for _, n := range d.names {
				p := path.Join(d.dir, n)
				if err = remotefs.RemoveAll(ctx, sc, p); err != nil {
					break
				}
				gone = append(gone, p)
			}
		}
		a.post(func() {
			cancel()
			d.busy = false
			a.filesChanged(d, made, gone, err)
			// A delete that failed half way asks again for what is left
			// alone: what it removed is no longer there to remove.
			if d.op == opDelete {
				d.names = slices.DeleteFunc(d.names, func(n string) bool {
					return slices.Contains(gone, path.Join(d.dir, n))
				})
			}
			if a.fileDlg != d {
				return
			}
			if err == nil || remotefs.IsCanceled(err) {
				a.fileDlg = nil
				return
			}
			d.err = err
		})
	}()
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
}

// filesChanged shows the panel of d's session as d left the server: the
// folder it happened in read again, and the tree and the current folder
// moved off what was renamed or deleted. made is a rename's or a new
// item's path, and gone what a delete removed, even when it failed half
// way.
func (a *App) filesChanged(d *fileDialog, made string, gone []string, err error) {
	s := d.sess
	f := &s.files
	cwd := f.cwd
	// moved moves the tree's folders under from to to, or forgets them
	// for "".
	moved := func(from, to string) {
		for k := range f.subdirs {
			if within(k, from) {
				delete(f.subdirs, k)
			}
		}
		var open []string
		for k := range f.tree.Open.All() {
			if within(k, from) {
				open = append(open, k)
			}
		}
		for _, k := range open {
			f.tree.Open.Remove(k)
			if to != "" {
				f.tree.Open.Add(to + strings.TrimPrefix(k, from))
			}
		}
	}
	switch d.op {
	case opRename:
		if err != nil {
			break
		}
		old := path.Join(d.dir, d.old)
		moved(old, made)
		if within(cwd, old) {
			cwd = made + strings.TrimPrefix(cwd, old)
		} else if cwd == d.dir {
			f.reveal = path.Base(made)
		}
	case opMkdir, opCreate:
		if err == nil && cwd == d.dir {
			f.reveal = path.Base(made)
		}
	case opDelete:
		for _, p := range gone {
			moved(p, "")
			if within(cwd, p) {
				cwd = d.dir
			}
		}
	}
	if _, loaded := f.subdirs[d.dir]; loaded && d.dir != cwd {
		a.loadSubdirs(s, d.dir)
	}
	if cwd != "" {
		a.openDir(s, cwd)
	}
}

// fileOpErrorText is what the file dialog says of err.
func fileOpErrorText(err error) string {
	var taken *remotefs.ExistsError
	switch {
	case errors.Is(err, remotefs.ErrBadName):
		return locales.BadFileName()
	case errors.As(err, &taken):
		return locales.FileNameTaken(taken.Name)
	}
	return err.Error()
}

// stemLength is the number of runes of name before its extension: all of
// it for a folder, or a name such as .bashrc.
func stemLength(name string, dir bool) int {
	if ext := path.Ext(name); !dir && ext != name {
		name = strings.TrimSuffix(name, ext)
	}
	return utf8.RuneCountInString(name)
}

// fileDialogView draws the file dialog, as a modal of the main window.
func (a *App) fileDialogView(c *ui.Context, d *fileDialog) {
	ui.Column(c).Width(400).Gap(12).Children(func() {
		ui.Text(c, d.title()).FontSize(16).Bold()
		submit := false
		empty := false
		if d.op == opDelete {
			body := locales.DeleteFilesBody(len(d.names))
			if d.dirs {
				body = locales.DeleteFoldersBody(len(d.names))
			}
			ui.Text(c, body)
		} else if d.op == opChmod {
			submit = a.permissionsForm(c, d)
			empty = d.octalBad
		} else {
			before := d.name
			in := ui.TextInput(c, &d.name).Label(locales.FolderName()).AutoFocus()
			if d.selectStem {
				d.selectStem = false
				in.SetTextSelection(0, stemLength(d.name, d.isDir))
			}
			submit = in.Submitted()
			if d.name != before {
				d.err = nil
			}
			empty = strings.TrimSpace(d.name) == ""
		}
		if d.err != nil {
			ui.Text(c, fileOpErrorText(d.err)).TextColor(theme.Danger).MaxLines(3)
		} else if d.octalBad {
			ui.Text(c, locales.BadMode()).TextColor(theme.Danger).MaxLines(3)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if d.busy {
				ui.Spinner(c)
				if d.op == opDelete {
					widgets.Muted(c, locales.DeletingFiles())
				}
			}
			ui.Spacer(c)
			if ui.Button(c, locales.Cancel()).Clicked() {
				a.closeFileDialog(d)
			}
			ok := locales.Ok()
			if d.op == opDelete {
				ok = locales.Delete()
			}
			if ui.PrimaryButton(c, ok).Disabled(d.busy || empty).Clicked() {
				submit = true
			}
		})
		if submit && !empty && a.fileDlg == d {
			a.runFileDialog(d)
		}
	})
}

// permissionsForm is the body of the permissions dialog: the owner's,
// the group's and the others' read, write and execute, with the octal
// number and the letters they make, kept in step with each other. It
// reports whether the octal field was submitted.
func (a *App) permissionsForm(c *ui.Context, d *fileDialog) bool {
	classes := []struct {
		name string
		read fs.FileMode // the class's read bit; write and execute follow
	}{
		{locales.PermissionsOwner(), 0o400},
		{locales.PermissionsGroup(), 0o040},
		{locales.PermissionsOther(), 0o004},
	}
	bits := []string{locales.PermissionsRead(), locales.PermissionsWrite(), locales.PermissionsExecute()}
	submit := false
	ui.Column(c).Gap(8).Children(func() {
		for _, cl := range classes {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cl.name).Width(64)
				for i, name := range bits {
					bit := cl.read >> i
					on := d.mode&bit != 0
					if ui.Checkbox(c, &on, name).Changed() {
						if on {
							d.mode |= bit
						} else {
							d.mode &^= bit
						}
						d.octal = modeOctal(d.mode)
						d.octalBad = false
					}
				}
			})
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.PermissionsOctal()).Width(64)
			in := ui.TextInput(c, &d.octal).Width(80).Label(locales.PermissionsOctal())
			submit = in.Submitted()
			if in.Changed() {
				if m, ok := parseOctalMode(d.octal, d.mode); ok {
					d.mode = m
					d.octalBad = false
				} else {
					d.octalBad = true
				}
			}
			ui.Text(c, d.mode.Perm().String()).Font("monospace").TextColor(theme.TextMuted)
		})
	})
	return submit
}

// modeOctal is m as octal digits: four when it has a set-ID or sticky
// bit, three otherwise.
func modeOctal(m fs.FileMode) string {
	special := 0
	if m&fs.ModeSetuid != 0 {
		special |= 4
	}
	if m&fs.ModeSetgid != 0 {
		special |= 2
	}
	if m&fs.ModeSticky != 0 {
		special |= 1
	}
	if special != 0 {
		return fmt.Sprintf("%d%03o", special, m.Perm())
	}
	return fmt.Sprintf("%03o", m.Perm())
}

// parseOctalMode reads three or four octal digits as a mode: with four,
// the first is the set-ID and sticky bits (4 setuid, 2 setgid, 1
// sticky); with three, they are kept from keep. Anything else fails.
func parseOctalMode(s string, keep fs.FileMode) (fs.FileMode, bool) {
	if len(s) != 3 && len(s) != 4 {
		return 0, false
	}
	var m fs.FileMode
	for _, r := range s {
		if r < '0' || r > '7' {
			return 0, false
		}
		m = m<<3 | fs.FileMode(r-'0')
	}
	if len(s) == 3 {
		return keep&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) | m, true
	}
	out := m & fs.ModePerm
	special := m >> 9
	if special&4 != 0 {
		out |= fs.ModeSetuid
	}
	if special&2 != 0 {
		out |= fs.ModeSetgid
	}
	if special&1 != 0 {
		out |= fs.ModeSticky
	}
	return out, true
}

// treeMenu is the context menu of the folder dir in the folder tree: it
// acts on dir, not on the current folder, and doesn't open it.
func (a *App) treeMenu(c *ui.Context, m *ui.Menu, s *Session, dir string) {
	f := &s.files
	if m.Item(locales.OpenFolder()).Chosen() {
		a.openDir(s, dir)
	}
	m.Separator()
	a.uploadMenuItems(m, s, dir)
	if dir != "/" && m.Item(locales.DownloadAsArchive()).Chosen() {
		a.downloadDirArchive(s, dir)
	}
	m.Separator()
	if m.Item(locales.NewFolder()).Chosen() {
		a.askNew(s, dir, opMkdir)
	}
	if m.Item(locales.NewFile()).Chosen() {
		a.askNew(s, dir, opCreate)
	}
	if dir != "/" {
		m.Separator()
		if m.Item(locales.Rename()).Chosen() {
			a.askRename(s, dir, true)
		}
		if m.Item(locales.Delete()).Chosen() {
			a.askDelete(s, path.Dir(dir), []string{path.Base(dir)}, true)
		}
	}
	m.Separator()
	if m.Item(locales.Refresh()).Chosen() {
		if dir == f.cwd {
			a.openDir(s, dir)
		} else {
			a.loadSubdirs(s, dir)
		}
	}
	if m.Item(locales.CopyPath()).Chosen() {
		c.WriteClipboard(dir)
	}
}
