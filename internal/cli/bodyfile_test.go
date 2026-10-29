package cli

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// --body-file never reads the secrets file, or anything else in the user config directory, however
// the path reaches it: an agent could otherwise ship every secret to the human's channels, and on
// to whoever reads them.
func TestBodyFileRefusesTheUserConfigDirectory(t *testing.T) {
	s, fk := notifySandbox(t)
	link := filepath.Join(t.TempDir(), "notes.txt")
	expect(t, os.Symlink(s.secretsFile(), link) == nil, "symlink")
	deep := write(t, filepath.Join(s.configDir(), "sub", "notes.txt"), "FAKE-DEEP-NOTE")
	deepLink := filepath.Join(t.TempDir(), "deep.txt")
	expect(t, os.Symlink(deep, deepLink) == nil, "symlink")
	hard := filepath.Join(s.home, "hard.txt")
	expect(t, os.Link(s.secretsFile(), hard) == nil, "hard link")
	for _, path := range []string{s.secretsFile(), link, deepLink, hard, s.userConfig(), filepath.Join(s.configDir(), "..", "yo-human", "secrets.yaml")} {
		r := s.notify("page", "--body-file", path)
		r.fails(t, "--body-file "+path, ExitUsage, "--body-file")
		mustContain(t, "stderr", r.err, "user config directory")
		mustNotContain(t, "stderr", r.err, "FAKE-")
	}
	expect(t, len(fk["phone"].Sent)+len(fk["pager"].Sent) == 0, "a message went out")
}

// Stored secret values never leave in a message, whichever field carries them: they are replaced
// before the message is queued, and a warning names the secret, never the value.
func TestStoredSecretValuesAreRedactedBeforeEnqueue(t *testing.T) {
	s, fk := notifySandbox(t)
	v := "FAKE-PD-CRITICAL" // the value notifySandbox stored for pd-critical
	r := s.do("", "notify", "--level", "page", "--title", "token "+v, "--ask", "rotate "+v, "--body", "the key is "+v,
		"--link", "https://example.com/x?k="+v, "--action", v+"=https://example.com/a?"+v, "--dedup-key", "k-"+v,
		"--project", "p-"+v, "--agent", "a-"+v)
	expect(t, r.code == ExitOK, "notify: %+v", r)
	mustContain(t, "stderr", r.err, "warning: ", "held the value of a secret")
	mustNotContain(t, "stderr", r.err, "pd-critical") // the warning never says which secret matched
	mustNotContain(t, "output", r.out+r.err, v)
	for _, name := range []string{"phone", "pager"} {
		for _, sent := range fk[name].Sent {
			mustNotContain(t, name+" message", messageText(sent), v)
		}
	}
	queued, _ := os.ReadDir(filepath.Join(s.stateDir(), "queue"))
	for _, q := range queued {
		b, _ := os.ReadFile(filepath.Join(s.stateDir(), "queue", q.Name()))
		mustNotContain(t, "queue entry", string(b), v)
	}
	s.do("", "notify", "--level", "page", "--title", "t", "--ask", "a", "--dedup-key", "plain")
	r = s.do("", "resolve", "--dedup-key", "plain", "--note", "rotated "+v)
	mustContain(t, "resolve", r.out, "RESULT resolved")
	mustContain(t, "resolve stderr", r.err, "held the value of a secret") // the note was sent, so it is said
	mustNotContain(t, "resolve output", r.out+r.err, v)
	for _, sent := range fk["phone"].Sent {
		mustNotContain(t, "resolve note", messageText(sent), v)
	}
}

// messageText is every text field of a message, joined.
func messageText(m channel.Message) string {
	parts := []string{m.Title, m.Ask, m.Body, m.Link, m.DedupKey, m.Project, m.Agent}
	for _, a := range m.Actions {
		parts = append(parts, a.Label, a.URL)
	}
	return strings.Join(parts, "\n")
}

// --body-file takes a regular file only, and never blocks: a FIFO nobody writes to, a terminal or
// a device is refused at once, before anything is read from it.
func TestBodyFileTakesARegularFileOnlyAndNeverBlocks(t *testing.T) {
	s, fk := notifySandbox(t)
	fifo := filepath.Join(t.TempDir(), "fifo")
	expect(t, syscall.Mkfifo(fifo, 0o600) == nil, "mkfifo")
	for _, path := range []string{fifo, "/dev/null", "/dev/zero", t.TempDir()} {
		done := make(chan result, 1)
		go func() { done <- s.notify("page", "--body-file", path) }()
		select {
		case r := <-done:
			r.fails(t, "--body-file "+path, ExitUsage, "not a regular file")
		case <-time.After(5 * time.Second):
			t.Fatalf("--body-file %s blocked", path)
		}
	}
	expect(t, len(fk["phone"].Sent)+len(fk["pager"].Sent) == 0, "a message went out")
}

// A regular file swapped for a FIFO between the check and the open is not opened blocking, and the
// file opened is refused as not the one checked.
func TestBodyFileSwappedForAFIFOIsRefusedWithoutBlocking(t *testing.T) {
	s, _ := notifySandbox(t)
	path := write(t, filepath.Join(t.TempDir(), "body.txt"), "a body")
	bodyOpenGap = func() {
		_ = os.Remove(path)
		_ = syscall.Mkfifo(path, 0o600)
	}
	t.Cleanup(func() { bodyOpenGap = nil })
	done := make(chan result, 1)
	go func() { done <- s.notify("page", "--body-file", path) }()
	select {
	case r := <-done:
		r.fails(t, "a swapped body", ExitUsage, "not a regular file")
	case <-time.After(5 * time.Second):
		t.Fatal("the open of a swapped-in FIFO blocked")
	}
}

// On a case-insensitive filesystem (the macOS default) another spelling of the user config
// directory is the same directory: the decision is by file identity, not by path text.
func TestBodyFileRefusesTheUserConfigDirectoryInAnyCase(t *testing.T) {
	s, fk := notifySandbox(t)
	write(t, filepath.Join(s.configDir(), "sub", "notes.txt"), "FAKE-DEEP-NOTE")
	upper := filepath.Join(s.home, ".CONFIG", "YO-HUMAN", "SUB", "NOTES.TXT")
	if _, err := os.Stat(upper); err != nil {
		t.Skip("the temp filesystem is case-sensitive")
	}
	for _, path := range []string{upper, filepath.Join(s.home, ".CONFIG", "YO-HUMAN", "SECRETS.YAML")} {
		r := s.notify("page", "--body-file", path)
		r.fails(t, "--body-file "+path, ExitUsage, "user config directory")
	}
	expect(t, len(fk["phone"].Sent)+len(fk["pager"].Sent) == 0, "a message went out")
}

// Every config layer the run reads is refused as a body too, wherever it lives: the --config file
// and the project file.
func TestBodyFileRefusesEveryConfigLayer(t *testing.T) {
	s, fk := notifySandbox(t)
	project := write(t, ".yo-human.yaml", "version: 1\n")
	explicit := write(t, filepath.Join(t.TempDir(), "explicit.yaml"), `version: 1
levels:
  - { name: page, use_when: "down", send: [ { channel: phone, interruption: active } ] }
`)
	s.noTTY()
	t.Setenv("YO_HUMAN_NONINTERACTIVE", "1")
	s.do("", "config", "trust", explicit).succeeds(t, "the human's script trusts the explicit file")
	t.Setenv("YO_HUMAN_NONINTERACTIVE", "")
	for _, path := range []string{project, explicit} {
		r := s.do("", "notify", "--level", "page", "--title", "t", "--ask", "a", "--body-file", path, "--config", explicit)
		r.fails(t, "--body-file "+path, ExitUsage, "--body-file")
		mustContain(t, "stderr", r.err, "config")
	}
	expect(t, len(fk["phone"].Sent)+len(fk["pager"].Sent) == 0, "a message went out")
}

// A hard link, made anywhere, to a file deep in the user config directory is that file; and a file
// in a directory of it that cannot be listed is still in it.
func TestBodyFileRefusesDeepFilesOfTheUserConfigDirectory(t *testing.T) {
	s, fk := notifySandbox(t)
	deep := write(t, filepath.Join(s.configDir(), "sub", "notes.txt"), "FAKE-DEEP-NOTE")
	hard := filepath.Join(t.TempDir(), "hard.txt")
	expect(t, os.Link(deep, hard) == nil, "hard link")
	locked := write(t, filepath.Join(s.configDir(), "locked", "notes.txt"), "FAKE-LOCKED-NOTE")
	expect(t, os.Chmod(filepath.Dir(locked), 0o300) == nil, "chmod")
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(locked), 0o700) })
	for _, path := range []string{hard, locked} {
		r := s.notify("page", "--body-file", path)
		r.fails(t, "--body-file "+path, ExitUsage, "user config directory")
	}
	expect(t, len(fk["phone"].Sent)+len(fk["pager"].Sent) == 0, "a message went out")
}

// The redaction warning is no oracle (fix round 2, L2): a call that sends nothing (a resolve of an
// unknown key, a suppressed repeat) says nothing about a secret, so guessing a value costs a page.
func TestRedactionWarnsOnlyWhenSomethingIsSent(t *testing.T) {
	s, _ := notifySandbox(t)
	v := "FAKE-PD-CRITICAL"
	r := s.do("", "resolve", "--dedup-key", "nothing-open", "--note", "guess "+v)
	expect(t, r.code == ExitOK && r.err == "", "a noop resolve with a secret in its note: %+v; want no warning", r)
	s.notify("page", "--dedup-key", "k", "--body", "guess "+v)
	r = s.notify("page", "--dedup-key", "k", "--body", "guess "+v)
	expect(t, strings.HasPrefix(r.out, "RESULT suppressed") && r.err == "", "a suppressed repeat with a secret: %+v; want no warning", r)
}

// --body-file reads only inside the working directory or the project (the directory holding the
// project config file): an agent cannot page the human's other files, such as an ssh key, to their
// channels (fix round 2, L5).
func TestBodyFileStaysInsideTheProject(t *testing.T) {
	s, fk := notifySandbox(t)
	key := write(t, filepath.Join(s.home, ".ssh", "id_ed25519"), "FAKE-SSH-KEY")
	cwd, _ := os.Getwd()
	link := filepath.Join(cwd, "notes.txt")
	expect(t, os.Symlink(key, link) == nil, "symlink")
	other := write(t, filepath.Join(t.TempDir(), "x.txt"), "a neighbour's file")
	for _, path := range []string{key, link, filepath.Join("..", filepath.Base(filepath.Dir(other)), "x.txt")} {
		r := s.notify("page", "--body-file", path)
		r.fails(t, "--body-file "+path, ExitUsage, "--body-file")
		mustContain(t, "fix", r.err, "copy the text into the project", "--body -")
	}
	expect(t, len(fk["pager"].Sent) == 0, "a message went out")

	inside := write(t, filepath.Join(cwd, "logs", "build.txt"), "the build log")
	s.notify("page", "--body-file", inside).succeeds2(t, "a file in the working directory")
	root := filepath.Join(cwd, "repo")
	write(t, filepath.Join(root, ".yo-human.yaml"), "version: 1\n") // untrusted: it still marks the project root
	notes := write(t, filepath.Join(root, "docs", "notes.txt"), "project notes")
	sub := filepath.Join(root, "src")
	expect(t, os.MkdirAll(sub, 0o700) == nil, "mkdir")
	t.Chdir(sub)
	r := s.notify("page", "--body-file", notes)
	expect(t, r.code == ExitOK, "a file in the project root, from a subdirectory: %+v", r)
}

// The project root is the directory where the search found the project file, never where a
// symlinked project file points: an agent can plant .yo-human.yaml -> $HOME/anything in its project,
// and that must not make the whole home the project (fix round 3, R2-M1).
func TestBodyFileProjectRootDoesNotFollowTheProjectFilesLink(t *testing.T) {
	s, fk := notifySandbox(t)
	write(t, filepath.Join(s.home, ".anyfile"), "anything")
	creds := write(t, filepath.Join(s.home, ".aws", "credentials"), "FAKE-AWS-KEY")
	cwd, _ := os.Getwd()
	expect(t, os.Symlink(filepath.Join(s.home, ".anyfile"), filepath.Join(cwd, ".yo-human.yaml")) == nil, "symlink")
	r := s.notify("page", "--body-file", creds)
	r.fails(t, "a body beside the project file's target", ExitUsage, "outside the working directory and the project")
	expect(t, len(fk["pager"].Sent) == 0, "a message went out")
}

// The post-open check needs the opened file to be a regular file AND the one that was checked.
// A swapped-in FIFO may be handed the freed inode number of the file it replaced, so SameFile can
// hold for it: the mode must be checked on its own. Real FileInfos make that deterministic: a FIFO
// compared with itself is the same file by dev and ino, and is not regular.
func TestSameRegularNeedsARegularFileThatIsTheSameFile(t *testing.T) {
	dir := t.TempDir()
	reg := write(t, filepath.Join(dir, "a.txt"), "a body")
	other := write(t, filepath.Join(dir, "b.txt"), "another")
	fifo := filepath.Join(dir, "fifo")
	expect(t, syscall.Mkfifo(fifo, 0o600) == nil, "mkfifo")
	info := func(path string) os.FileInfo {
		fi, err := os.Stat(path)
		expect(t, err == nil, "stat %s: %v", path, err)
		return fi
	}
	cases := []struct {
		name          string
		before, after os.FileInfo
		want          bool
	}{
		{"the same regular file", info(reg), info(reg), true},
		{"a different regular file", info(reg), info(other), false},
		{"a FIFO in place of a regular file", info(reg), info(fifo), false},
		{"a FIFO that is the same file (a reused inode) but not regular", info(fifo), info(fifo), false},
	}
	for _, c := range cases {
		expect(t, os.SameFile(c.before, c.after) == (c.name == "the same regular file" || c.name == "a FIFO that is the same file (a reused inode) but not regular"), "%s: the case does not hold the SameFile it claims", c.name)
		expect(t, sameRegular(c.before, c.after) == c.want, "%s: sameRegular = %v, want %v", c.name, !c.want, c.want)
	}
}
