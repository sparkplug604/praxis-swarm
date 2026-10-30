package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ResolveResources returns the deduplicated union of original logical resources
// and canonical filesystem resources. A caller must atomically reserve the whole
// result. A global physical-path namespace detects overlap even between nested
// independent repositories. Spelling is resolved by device/inode identity, never
// assumed case or Unicode rules. New paths reserve their nearest existing ancestor
// because filesystem alias behavior for a nonexistent entry cannot be observed.
// Internal symlinks are conservatively unsupported, as are any files with multiple hard links.
func ResolveResources(ctx context.Context, sourceRoot string, resources []domain.SwarmResource) ([]domain.SwarmResource, error) {
	identity, err := identifyRoot(sourceRoot)
	if err != nil {
		return nil, err
	}
	if err := requireRepositoryRoot(ctx, identity); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(identity.Path)
	if err != nil {
		return nil, err
	}
	// Resource resolution only reads directory entries through this handle.
	defer func() { _ = r.Close() }()
	set := map[domain.SwarmResource]bool{}
	for _, resource := range resources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !domain.SwarmValidID(resource.Namespace) || !domain.SwarmValidID(resource.Key) {
			return nil, fmt.Errorf("%w: resource identity", ErrUnsafe)
		}
		set[resource] = true
		switch resource.Kind {
		case "semantic":
			continue
		case "path":
			if !validPath(resource.Key) || strings.ContainsAny(resource.Key, "*?[]:") {
				return nil, fmt.Errorf("%w: resource path", ErrUnsafe)
			}
		default:
			return nil, fmt.Errorf("%w: resource kind", ErrUnsafe)
		}
		actual, _, err := resolvePath(r, resource.Key)
		if err != nil {
			return nil, err
		}
		if err := checkScope(ctx, r, actual); err != nil {
			return nil, err
		}
		key := strings.TrimPrefix(filepath.ToSlash(filepath.Join(identity.Path, actual)), "/")
		if key == "" {
			key = "."
		}
		if !domain.SwarmValidID(key) || !validPath(key) || strings.ContainsAny(key, "*?[]:") {
			return nil, fmt.Errorf("%w: canonical resource path exceeds domain limits", ErrUnsupported)
		}
		set[domain.SwarmResource{Namespace: "fs:path", Kind: "path", Key: key}] = true
	}
	if err := validateRoot(identity); err != nil {
		return nil, err
	}
	out := make([]domain.SwarmResource, 0, len(set))
	for resource := range set {
		out = append(out, resource)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.Namespace+"\x00"+a.Kind+"\x00"+a.Key < b.Namespace+"\x00"+b.Kind+"\x00"+b.Key
	})
	return out, nil
}

func checkScope(ctx context.Context, root *os.Root, start string) error {
	return fs.WalkDir(root.FS(), start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.EqualFold(path.Base(p), ".git") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := root.Lstat(p)
		if err != nil {
			return err
		}
		if !validPath(p) {
			return fmt.Errorf("%w: scope path", ErrUnsafe)
		}
		return safeInfo(info)
	})
}
