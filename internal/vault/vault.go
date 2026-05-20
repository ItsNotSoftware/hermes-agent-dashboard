// Package vault returns safe metadata about the Obsidian vault: counts,
// total bytes, most-recent notes. It never reads note contents.
package vault

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Note struct {
	Name      string `json:"name"`
	UpdatedAt string `json:"updated_at"`
}

type Intel struct {
	Path           string `json:"path"`
	Present        bool   `json:"present"`
	NoteCount      int    `json:"note_count"`
	VaultSizeBytes int64  `json:"vault_size_bytes"`
	RecentNotes    []Note `json:"recent_notes"`
	KSPNotes       []Note `json:"ksp_notes"`
}

// Path returns the conventional vault location (~/Documents/Obsidian Vault).
func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "Obsidian Vault")
}

// HermesPath returns the Hermes subdirectory inside the vault.
func HermesPath() string { return filepath.Join(Path(), "Hermes") }

func Scan() Intel {
	root := Path()
	info, err := os.Stat(root)
	intel := Intel{Path: root, Present: err == nil && info != nil && info.IsDir()}
	if !intel.Present {
		return intel
	}

	var notes, ksp []noteEntry
	var total int64

	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return nil
		}
		total += st.Size()
		if !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		name := strings.TrimSuffix(rel, filepath.Ext(rel))
		e := noteEntry{name: name, mtime: st.ModTime()}
		notes = append(notes, e)
		if strings.Contains(strings.ToLower(name), "ksp") {
			ksp = append(ksp, e)
		}
		return nil
	})

	sort.Slice(notes, func(i, j int) bool { return notes[i].mtime.After(notes[j].mtime) })
	sort.Slice(ksp, func(i, j int) bool { return ksp[i].mtime.After(ksp[j].mtime) })

	intel.NoteCount = len(notes)
	intel.VaultSizeBytes = total
	intel.RecentNotes = takeNotes(notes, 6)
	intel.KSPNotes = takeNotes(ksp, 4)
	return intel
}

type noteEntry struct {
	name  string
	mtime time.Time
}

func takeNotes(src []noteEntry, n int) []Note {
	if n > len(src) {
		n = len(src)
	}
	out := make([]Note, n)
	for i := 0; i < n; i++ {
		out[i] = Note{Name: src[i].name, UpdatedAt: src[i].mtime.Format(time.RFC3339)}
	}
	return out
}
