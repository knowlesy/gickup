package local

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cooperspencer/gickup/types"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// threeDays keeps planted backups on different calendar days in any time zone.
const threeDays = 3 * 24 * 60 * 60

// newSourceRepo creates a git repository with one commit to back up.
func newSourceRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git.PlainInit() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree() error = %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if _, err := wt.Commit("seed", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()},
	}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	return dir
}

// plantBackups creates older backups in parentdir, numbered from newest (1) to oldest (count),
// three days apart. It returns their names by number.
func plantBackups(t *testing.T, parentdir string, count int, zipped bool, issues ...int) map[int][]string {
	t.Helper()

	if err := os.MkdirAll(parentdir, 0o777); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	planted := map[int][]string{}
	for k := 1; k <= count; k++ {
		ts := fmt.Sprint(now - int64(k)*threeDays)
		if zipped {
			if err := os.WriteFile(filepath.Join(parentdir, ts+".zip"), []byte("planted"), 0o644); err != nil {
				t.Fatal(err)
			}
			planted[k] = append(planted[k], ts+".zip")
		} else {
			if err := os.Mkdir(filepath.Join(parentdir, ts), 0o777); err != nil {
				t.Fatal(err)
			}
			planted[k] = append(planted[k], ts)
		}
		for _, i := range issues {
			if i == k {
				if err := os.Mkdir(filepath.Join(parentdir, ts+".issues"), 0o777); err != nil {
					t.Fatal(err)
				}
				planted[k] = append(planted[k], ts+".issues")
			}
		}
	}

	return planted
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, f := range files {
		names = append(names, f.Name())
	}

	return names
}

// newBackups returns the entries in after that are not in before.
func newBackups(before, after []string) []string {
	seen := map[string]bool{}
	for _, n := range before {
		seen[n] = true
	}
	added := []string{}
	for _, n := range after {
		if !seen[n] {
			added = append(added, n)
		}
	}

	return added
}

func runLocally(t *testing.T, repo types.Repo, l types.Local, dry bool) {
	t.Helper()

	if !Locally(repo, l, dry) {
		t.Fatal("Locally() = false, want true")
	}
}

func assertRemaining(t *testing.T, got []string, want ...[]string) {
	t.Helper()

	all := []string{}
	for _, w := range want {
		all = append(all, w...)
	}
	sort.Strings(all)
	if !reflect.DeepEqual(got, all) {
		t.Fatalf("remaining = %v, want %v", got, all)
	}
}

//nolint:paralleltest // Locally assigns the package-level logger.
func TestLocallyTieredZipPrunesOldArchives(t *testing.T) {
	dest := t.TempDir()
	parentdir := filepath.Join(dest, "testrepo")
	planted := plantBackups(t, parentdir, 5, true, 1, 2)
	if err := os.WriteFile(filepath.Join(parentdir, "notes.txt"), []byte("not a backup"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := listDir(t, parentdir)

	runLocally(t, types.Repo{Name: "testrepo", URL: newSourceRepo(t)},
		types.Local{Path: dest, Zip: true, KeepDaily: 2}, false)

	after := listDir(t, parentdir)
	added := newBackups(before, after)
	if len(added) != 1 || !strings.HasSuffix(added[0], ".zip") {
		t.Fatalf("new entries = %v, want one <timestamp>.zip", added)
	}
	assertRemaining(t, after, added, planted[1], []string{"notes.txt"})

	stamp := strings.TrimSuffix(added[0], ".zip")
	r, err := zip.OpenReader(filepath.Join(parentdir, added[0]))
	if err != nil {
		t.Fatalf("zip.OpenReader() error = %v", err)
	}
	defer r.Close()
	found := false
	for _, f := range r.File {
		if f.Name == stamp+"/README.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%s does not contain %s/README.md", added[0], stamp)
	}
}

//nolint:paralleltest // Locally assigns the package-level logger.
func TestLocallyTieredWithoutZipPrunesOldDirectories(t *testing.T) {
	dest := t.TempDir()
	parentdir := filepath.Join(dest, "testrepo")
	planted := plantBackups(t, parentdir, 5, false, 1, 3)
	before := listDir(t, parentdir)

	runLocally(t, types.Repo{Name: "testrepo", URL: newSourceRepo(t)},
		types.Local{Path: dest, KeepDaily: 3}, false)

	after := listDir(t, parentdir)
	added := newBackups(before, after)
	if len(added) != 1 {
		t.Fatalf("new entries = %v, want one <timestamp> directory", added)
	}
	assertRemaining(t, after, added, planted[1], planted[2])

	if _, err := git.PlainOpen(filepath.Join(parentdir, added[0])); err != nil {
		t.Fatalf("new backup is not a git repository: %v", err)
	}
}

//nolint:paralleltest // Locally assigns the package-level logger.
func TestLocallyLegacyKeepStillPrunes(t *testing.T) {
	dest := t.TempDir()
	parentdir := filepath.Join(dest, "testrepo")
	planted := plantBackups(t, parentdir, 4, true)
	before := listDir(t, parentdir)

	runLocally(t, types.Repo{Name: "testrepo", URL: newSourceRepo(t)},
		types.Local{Path: dest, Zip: true, Keep: 2}, false)

	after := listDir(t, parentdir)
	assertRemaining(t, after, newBackups(before, after), planted[1])
}

//nolint:paralleltest // Locally assigns the package-level logger.
func TestLocallyTieredLayouts(t *testing.T) {
	cases := []struct {
		name      string
		l         types.Local
		parentdir []string
	}{
		{"structured", types.Local{Structured: true}, []string{"local", "git", "testrepo"}},
		{"bare", types.Local{Bare: true}, []string{"testrepo.git"}},
		{"bare zip", types.Local{Bare: true, Zip: true}, []string{"testrepo.git"}},
		{"mirror", types.Local{Mirror: true}, []string{"testrepo.git"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dest := t.TempDir()
			parentdir := filepath.Join(append([]string{dest}, c.parentdir...)...)
			plantBackups(t, parentdir, 2, c.l.Zip)
			before := listDir(t, parentdir)

			l := c.l
			l.Path = dest
			l.KeepDaily = 1
			runLocally(t, types.Repo{Name: "testrepo", URL: newSourceRepo(t), Hoster: "local", Owner: "git"}, l, false)

			after := listDir(t, parentdir)
			added := newBackups(before, after)
			if len(added) != 1 {
				t.Fatalf("new entries in %s = %v, want one backup", parentdir, added)
			}
			assertRemaining(t, after, added)
		})
	}
}

//nolint:paralleltest // Locally assigns the package-level logger.
func TestLocallyDryRunChangesNothing(t *testing.T) {
	for _, zipped := range []bool{false, true} {
		t.Run(fmt.Sprintf("zip=%v", zipped), func(t *testing.T) {
			dest := t.TempDir()
			parentdir := filepath.Join(dest, "testrepo")
			plantBackups(t, parentdir, 4, zipped, 3)
			before := listDir(t, parentdir)

			for _, l := range []types.Local{
				{Path: dest, Zip: zipped, Keep: 1},
				{Path: dest, Zip: zipped, KeepDaily: 1},
			} {
				runLocally(t, types.Repo{Name: "testrepo", URL: newSourceRepo(t)}, l, true)

				if after := listDir(t, parentdir); !reflect.DeepEqual(after, before) {
					t.Fatalf("dry run with %+v changed %v to %v", l, before, after)
				}
			}
		})
	}
}
