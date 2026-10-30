package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"golang.org/x/text/unicode/norm"
)

func command(t *testing.T, root string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return out
}
func put(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
func repo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	command(t, root, "init", "-q", "--initial-branch=main")
	return root
}

func TestCaptureProtectsLinkedWorktreeGitMetadata(t *testing.T) {
	root := repo(t)
	put(t, root, "input", "preserved")
	command(t, root, "add", "input")
	command(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "fixture")
	linked := filepath.Join(filepath.Dir(root), "linked")
	command(t, root, "-c", "core.hooksPath=/dev/null", "worktree", "add", "--detach", linked, "HEAD")
	gitDir := strings.TrimSpace(string(command(t, linked, "rev-parse", "--absolute-git-dir")))
	common := strings.TrimSpace(string(command(t, linked, "rev-parse", "--path-format=absolute", "--git-common-dir")))
	for _, destination := range []string{filepath.Join(gitDir, "copy"), filepath.Join(common, "copy")} {
		if _, err := Capture(context.Background(), CaptureRequest{SourceRoot: linked, Destination: destination}); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Git metadata destination accepted %s: %v", destination, err)
		}
		if _, err := os.Lstat(destination); !os.IsNotExist(err) {
			t.Fatalf("metadata was modified: %v", err)
		}
	}
	s := capture(t, CaptureRequest{SourceRoot: linked, Destination: filepath.Join(filepath.Dir(root), "safe-worker")})
	if s.Source.GitDirectory.Path == s.Source.GitCommonDirectory.Path {
		t.Fatal("fixture did not exercise separate Git roots")
	}
}
func request(root string) CaptureRequest {
	return CaptureRequest{SourceRoot: root, Destination: filepath.Join(filepath.Dir(root), "worker")}
}
func capture(t *testing.T, req CaptureRequest) Snapshot {
	t.Helper()
	s, err := Capture(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func entry(t *testing.T, s Snapshot, p string) Entry {
	t.Helper()
	for _, e := range s.Source.Entries {
		if e.Path == p {
			return e
		}
	}
	t.Fatalf("missing manifest path %q", p)
	return Entry{}
}
func physical(resources []domain.SwarmResource) []domain.SwarmResource {
	var out []domain.SwarmResource
	for _, r := range resources {
		if strings.HasPrefix(r.Namespace, "fs:") {
			out = append(out, r)
		}
	}
	return out
}

func TestCapturePreservesDirtyTrackedUntrackedAndIndex(t *testing.T) {
	root := repo(t)
	put(t, root, "tracked.txt", "staged bytes")
	put(t, root, "deleted.txt", "deleted bytes")
	put(t, root, "bin/tool", "#!/bin/sh\nexit 0\n")
	put(t, root, ".gitignore", "ignored/\n")
	if err := os.Chmod(filepath.Join(root, "bin/tool"), 0755); err != nil {
		t.Fatal(err)
	}
	command(t, root, "add", ".")
	put(t, root, "tracked.txt", "live dirty bytes")
	put(t, root, "loose.txt", "untracked bytes")
	put(t, root, "ignored/needed.txt", "explicit ignored bytes")
	put(t, root, "ignored/secret.txt", "excluded bytes")
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, ".git/index")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	req := request(root)
	req.IncludeIgnored = []string{"ignored/needed.txt"}
	s := capture(t, req)
	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !bytes.Equal(before, s.Source.IndexBytes) || !os.SameFile(beforeInfo, afterInfo) || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatal("source index changed or was not preserved")
	}
	if !entry(t, s, "deleted.txt").Missing {
		t.Fatal("deletion was lost")
	}
	for p, want := range map[string]string{"tracked.txt": "live dirty bytes", "loose.txt": "untracked bytes", "ignored/needed.txt": "explicit ignored bytes"} {
		got, err := os.ReadFile(filepath.Join(s.Destination.Path, p))
		if err != nil || string(got) != want {
			t.Fatalf("copy %s: %s, %v", p, got, err)
		}
		src, err := os.Stat(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		dst, err := os.Stat(filepath.Join(s.Destination.Path, p))
		if err != nil {
			t.Fatal(err)
		}
		if os.SameFile(src, dst) {
			t.Fatalf("copy %s reused source inode", p)
		}
	}
	for _, p := range []string{".git", "deleted.txt", "ignored/secret.txt"} {
		if _, err := os.Lstat(filepath.Join(s.Destination.Path, p)); !os.IsNotExist(err) {
			t.Fatalf("excluded path %s: %v", p, err)
		}
	}
	mode, err := os.Stat(filepath.Join(s.Destination.Path, "bin/tool"))
	if err != nil || mode.Mode().Perm() != 0755 {
		t.Fatalf("executable mode: %v, %v", mode, err)
	}
	put(t, s.Destination.Path, "tracked.txt", "candidate change")
	if err := ValidateForLaunch(context.Background(), s); !errors.Is(err, ErrChanged) {
		t.Fatalf("launch accepted changed candidate: %v", err)
	}
	sub, err := ValidateSubmission(context.Background(), s)
	if err != nil || sub.SnapshotHash != s.IdentityHash || sub.Hash == "" {
		t.Fatalf("submission: %+v, %v", sub, err)
	}
	got, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil || string(got) != "live dirty bytes" {
		t.Fatalf("worker changed source: %s, %v", got, err)
	}
}

func TestCaptureExplicitIgnoredDirectoryAndEmptyRepository(t *testing.T) {
	t.Run("ignored directory", func(t *testing.T) {
		root := repo(t)
		put(t, root, ".gitignore", "ignored/\n")
		put(t, root, "ignored/sub/needed", "data")
		req := request(root)
		req.IncludeIgnored = []string{"ignored"}
		s := capture(t, req)
		entry(t, s, "ignored/sub/needed")
	})
	t.Run("empty repository", func(t *testing.T) {
		root := repo(t)
		s := capture(t, request(root))
		if !s.Source.Index.Missing {
			t.Fatal("unexpected index")
		}
	})
}

func TestDestinationExclusivityAndOverlap(t *testing.T) {
	for _, kind := range []string{"source", "descendant", "ancestor", "existing"} {
		t.Run(kind, func(t *testing.T) {
			root := repo(t)
			put(t, root, "input", "data")
			req := request(root)
			switch kind {
			case "source":
				req.Destination = root
			case "descendant":
				req.Destination = filepath.Join(root, "worker")
			case "ancestor":
				req.Destination = filepath.Dir(root)
			case "existing":
				if err := os.Mkdir(req.Destination, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Capture(context.Background(), req); err == nil {
				t.Fatal("unsafe destination accepted")
			}
			if got, err := os.ReadFile(filepath.Join(root, "input")); err != nil || string(got) != "data" {
				t.Fatal("source damaged")
			}
		})
	}
}

func TestSourceAndCandidateLinkRejections(t *testing.T) {
	for _, boundary := range []string{"capture", "submission"} {
		for _, kind := range []string{"escaping symlink", "internal symlink", "hardlink"} {
			t.Run(boundary+"/"+kind, func(t *testing.T) {
				root := repo(t)
				put(t, root, "input", "data")
				location := root
				var s Snapshot
				if boundary == "submission" {
					s = capture(t, request(root))
					location = s.Destination.Path
				}
				alias := filepath.Join(location, "alias")
				switch kind {
				case "escaping symlink":
					if err := os.Symlink(filepath.Dir(root), alias); err != nil {
						t.Fatal(err)
					}
				case "internal symlink":
					if err := os.Symlink("input", alias); err != nil {
						t.Fatal(err)
					}
				case "hardlink":
					if err := os.Link(filepath.Join(location, "input"), alias); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				if boundary == "capture" {
					_, err = Capture(context.Background(), request(root))
				} else {
					_, err = ValidateSubmission(context.Background(), s)
				}
				if !errors.Is(err, ErrUnsafe) {
					t.Fatalf("link accepted: %v", err)
				}
			})
		}
	}
}

func TestSourceDriftAndRootReplacement(t *testing.T) {
	for _, kind := range []string{"source bytes", "index bytes", "source root", "destination root", "source moved", "destination moved", "snapshot binding"} {
		t.Run(kind, func(t *testing.T) {
			root := repo(t)
			put(t, root, "input", "staged")
			command(t, root, "add", "input")
			s := capture(t, request(root))
			switch kind {
			case "source bytes":
				put(t, root, "input", "changed")
			case "index bytes":
				put(t, root, "new", "stage")
				command(t, root, "add", "new")
			case "source root", "destination root", "source moved", "destination moved":
				target := root
				if strings.HasPrefix(kind, "destination") {
					target = s.Destination.Path
				}
				if err := os.Rename(target, target+"-moved"); err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(kind, "root") {
					if err := os.Mkdir(target, 0755); err != nil {
						t.Fatal(err)
					}
				}
			case "snapshot binding":
				s.Source.IndexBytes = append(s.Source.IndexBytes, 1)
			}
			if _, err := ValidateSubmission(context.Background(), s); err == nil {
				t.Fatal("changed binding accepted")
			}
		})
	}
}

// The context callback deterministically changes the source after the exclusive
// destination is created, avoiding timing-dependent race tests or test hooks in
// production capture code.
type mutationContext struct {
	context.Context
	once        sync.Once
	destination string
	mutate      func()
}

func (c *mutationContext) Err() error {
	if _, err := os.Stat(c.destination); err == nil {
		c.once.Do(c.mutate)
	}
	return c.Context.Err()
}
func TestSourceMutationDuringCaptureFails(t *testing.T) {
	root := repo(t)
	put(t, root, "input", "before")
	req := request(root)
	ctx := &mutationContext{Context: context.Background(), destination: req.Destination, mutate: func() { put(t, root, "input", "after") }}
	if _, err := Capture(ctx, req); !errors.Is(err, ErrChanged) {
		t.Fatalf("concurrent mutation accepted: %v", err)
	}
}

func TestResourcesPhysicalIdentityAndConservativeMissingPaths(t *testing.T) {
	root := repo(t)
	put(t, root, "src/main.go", "source")
	resource := domain.SwarmResource{Namespace: "project-a", Kind: "path", Key: "src/main.go"}
	first, err := ResolveResources(context.Background(), root, []domain.SwarmResource{resource})
	if err != nil {
		t.Fatal(err)
	}
	resource.Namespace = "different-logical-project"
	second, err := ResolveResources(context.Background(), root, []domain.SwarmResource{resource})
	if err != nil {
		t.Fatal(err)
	}
	a, b := physical(first), physical(second)
	if len(first) != 2 || len(a) != 1 || len(b) != 1 || a[0] != b[0] {
		t.Fatalf("physical identity not shared: %+v %+v", first, second)
	}
	resource.Key = "src/new/Thing.go"
	missing, err := ResolveResources(context.Background(), root, []domain.SwarmResource{resource})
	if err != nil {
		t.Fatal(err)
	}
	if p := physical(missing); len(p) != 1 || !strings.HasSuffix(p[0].Key, "/src") || !domain.SwarmResourcesConflict(p[0], a[0]) {
		t.Fatalf("missing path underlocked: %+v", missing)
	}
	resource.Key = "new.go"
	missing, err = ResolveResources(context.Background(), root, []domain.SwarmResource{resource})
	if err != nil {
		t.Fatal(err)
	}
	id, err := identifyRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if physical(missing)[0].Key != strings.TrimPrefix(filepath.ToSlash(id.Path), "/") {
		t.Fatalf("missing top-level path underlocked: %+v", missing)
	}
	semantic := domain.SwarmResource{Namespace: "project-a", Kind: "semantic", Key: "invariant"}
	resolved, err := ResolveResources(context.Background(), root, []domain.SwarmResource{semantic, semantic})
	if err != nil || len(resolved) != 1 || resolved[0] != semantic {
		t.Fatalf("semantic resources: %+v %v", resolved, err)
	}
}

func TestResourcesRejectFilesystemAliases(t *testing.T) {
	for _, kind := range []string{"internal symlink", "escaping symlink", "hardlink", "directory with hardlink", "metadata"} {
		t.Run(kind, func(t *testing.T) {
			root := repo(t)
			put(t, root, "src/input", "data")
			key := "alias"
			switch kind {
			case "internal symlink":
				if err := os.Symlink("src/input", filepath.Join(root, key)); err != nil {
					t.Fatal(err)
				}
			case "escaping symlink":
				if err := os.Symlink(filepath.Dir(root), filepath.Join(root, key)); err != nil {
					t.Fatal(err)
				}
			case "hardlink", "directory with hardlink":
				if err := os.Link(filepath.Join(root, "src/input"), filepath.Join(root, key)); err != nil {
					t.Fatal(err)
				}
				if kind == "directory with hardlink" {
					key = "src"
				}
			case "metadata":
				key = ".git/index"
			}
			_, err := ResolveResources(context.Background(), root, []domain.SwarmResource{{Namespace: "repo", Kind: "path", Key: key}})
			if !errors.Is(err, ErrUnsafe) {
				t.Fatalf("unsafe alias accepted: %v", err)
			}
		})
	}
}

func TestActualVolumeCaseAndUnicodeIdentity(t *testing.T) {
	root := repo(t)
	put(t, root, "MixedCase.txt", "case")
	nfc := "caf\u00e9.txt"
	nfd := norm.NFD.String(nfc)
	put(t, root, nfc, "unicode")
	for _, pair := range [][2]string{{"MixedCase.txt", "mixedcase.txt"}, {nfc, nfd}} {
		original, err := os.Stat(filepath.Join(root, pair[0]))
		if err != nil {
			t.Fatal(err)
		}
		alias, err := os.Stat(filepath.Join(root, pair[1]))
		same := err == nil && os.SameFile(original, alias)
		t.Logf("actual %s volume alias %q / %q: same_inode=%v", runtime.GOOS, pair[0], pair[1], same)
		if !same {
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			continue
		}
		a, err := ResolveResources(context.Background(), root, []domain.SwarmResource{{Namespace: "repo", Kind: "path", Key: pair[0]}})
		if err != nil {
			t.Fatal(err)
		}
		b, err := ResolveResources(context.Background(), root, []domain.SwarmResource{{Namespace: "repo", Kind: "path", Key: pair[1]}})
		if err != nil {
			t.Fatal(err)
		}
		if physical(a)[0] != physical(b)[0] {
			t.Fatalf("actual alias received different locks: %+v %+v", a, b)
		}
	}
}

func TestCaseSensitiveVolumeDistinctFiles(t *testing.T) {
	fixture := os.Getenv("SWARM_CASE_SENSITIVE_ROOT")
	var root string
	if fixture == "" {
		root = t.TempDir()
	} else {
		var err error
		root, err = os.MkdirTemp(fixture, "swarm-case-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(root) })
	}
	put(t, root, "Case.txt", "upper")
	command(t, root, "init", "-q", "--initial-branch=main")
	f, err := os.OpenFile(filepath.Join(root, "case.txt"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if os.IsExist(err) {
		t.Skip("actual volume is case-insensitive; no case-sensitive fixture configured")
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("lower"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	a, err := ResolveResources(context.Background(), root, []domain.SwarmResource{{Namespace: "repo", Kind: "path", Key: "Case.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ResolveResources(context.Background(), root, []domain.SwarmResource{{Namespace: "repo", Kind: "path", Key: "case.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if domain.SwarmResourcesConflict(physical(a)[0], physical(b)[0]) {
		t.Fatalf("distinct files incorrectly alias: %+v %+v", a, b)
	}
}

func TestResourcesNestedIndependentRepositoriesConflict(t *testing.T) {
	root := repo(t)
	put(t, root, ".gitignore", "nested/\n")
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}
	command(t, nested, "init", "-q", "--initial-branch=main")
	put(t, nested, "input", "data")
	parent, err := ResolveResources(context.Background(), root, []domain.SwarmResource{{Namespace: "parent", Kind: "path", Key: "nested"}})
	if err != nil {
		t.Fatal(err)
	}
	child, err := ResolveResources(context.Background(), nested, []domain.SwarmResource{{Namespace: "child", Kind: "path", Key: "input"}})
	if err != nil {
		t.Fatal(err)
	}
	if !domain.SwarmResourcesConflict(physical(parent)[0], physical(child)[0]) {
		t.Fatalf("nested roots evade lock: %+v %+v", parent, child)
	}
	put(t, root, "ordinary/sub/input", "data")
	_, err = ResolveResources(context.Background(), filepath.Join(root, "ordinary"), []domain.SwarmResource{{Namespace: "subdir", Kind: "path", Key: "sub/input"}})
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("non-root source accepted: %v", err)
	}
}

func TestUnsupportedRecoveryInputs(t *testing.T) {
	t.Run("split index", func(t *testing.T) {
		root := repo(t)
		put(t, root, "input", "data")
		command(t, root, "add", "input")
		command(t, root, "update-index", "--split-index")
		if _, err := Capture(context.Background(), request(root)); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("split index accepted: %v", err)
		}
	})
	t.Run("git metadata input", func(t *testing.T) {
		root := repo(t)
		req := request(root)
		req.IncludeIgnored = []string{".git/config"}
		if _, err := Capture(context.Background(), req); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("metadata accepted: %v", err)
		}
	})
}

func TestCaptureRespectsConfiguredGlobalIgnores(t *testing.T) {
	root := repo(t)
	configRoot := t.TempDir()
	put(t, configRoot, "global-ignore", "private.txt\n")
	put(t, configRoot, "gitconfig", "[core]\n\texcludesFile = "+filepath.Join(configRoot, "global-ignore")+"\n")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(configRoot, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	put(t, root, "private.txt", "excluded by real Git configuration")
	put(t, root, "public.txt", "visible")
	s := capture(t, request(root))
	if _, err := os.Stat(filepath.Join(s.Destination.Path, "private.txt")); !os.IsNotExist(err) {
		t.Fatalf("globally ignored input copied: %v", err)
	}
	req := request(root)
	req.Destination += "-explicit"
	req.IncludeIgnored = []string{"private.txt"}
	explicit := capture(t, req)
	if _, err := os.Stat(filepath.Join(explicit.Destination.Path, "private.txt")); err != nil {
		t.Fatalf("explicit global ignored input absent: %v", err)
	}
}
