package qualification

import (
	"fmt"
	"os"
	"path/filepath"
)

// Directory handles are held across verification to avoid accepting a replaced
// path merely because its string is unchanged. This is a qualification check,
// not the OS enforcement the dispatch runtime must provide.
type directoryBinding struct {
	OriginalPath  string `json:"originalPath"`
	CanonicalPath string `json:"canonicalPath"`
	FileID        string `json:"fileId"`
}

type directorySnapshot struct {
	binding directoryBinding
	handle  *os.File
}

func validatePaths(req *Request) (directories []directorySnapshot, err error) {
	defer func() {
		if err != nil {
			closeDirectories(directories)
		}
	}()
	paths := []*string{&req.Launch.WorkspacePath, &req.Permissions.CoordinatorRoot, &req.Permissions.EvaluatorRoot}
	if req.Launch.DataDir != "" {
		paths = append(paths, &req.Launch.DataDir)
	}
	for _, path := range paths {
		snapshot, openErr := openDirectory(*path)
		if openErr != nil {
			return directories, openErr
		}
		directories = append(directories, snapshot)
		*path = snapshot.binding.CanonicalPath
	}
	for _, protected := range []string{req.Permissions.CoordinatorRoot, req.Permissions.EvaluatorRoot} {
		if overlaps(req.Launch.WorkspacePath, protected) || (req.Launch.DataDir != "" && overlaps(req.Launch.DataDir, protected)) {
			return directories, fmt.Errorf("%w: workspace and agent data must be separate from coordinator/evaluator storage", ErrUnsafePermissions)
		}
	}
	if req.Role == Reviewer && !validDigest(req.Permissions.SubmissionSHA256) {
		return directories, fmt.Errorf("%w: reviewer requires the frozen submission SHA-256", ErrInvalid)
	}
	return directories, nil
}

func openDirectory(path string) (directorySnapshot, error) {
	if !filepath.IsAbs(path) {
		return directorySnapshot{}, fmt.Errorf("%w: workspace and protected storage paths must be absolute and existing", ErrInvalid)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return directorySnapshot{}, fmt.Errorf("%w: cannot resolve required directory: %v", ErrInvalid, err)
	}
	handle, err := os.Open(canonical)
	if err != nil {
		return directorySnapshot{}, fmt.Errorf("%w: cannot open required directory: %v", ErrInvalid, err)
	}
	info, err := handle.Stat()
	if err != nil || !info.IsDir() {
		_ = handle.Close()
		return directorySnapshot{}, fmt.Errorf("%w: workspace and storage roots must be directories", ErrInvalid)
	}
	id, err := directoryFileID(handle, info)
	if err != nil {
		_ = handle.Close()
		return directorySnapshot{}, fmt.Errorf("%w: cannot identify directory: %v", ErrUnverified, err)
	}
	return directorySnapshot{directoryBinding{path, filepath.Clean(canonical), id}, handle}, nil
}

func revalidateDirectories(directories []directorySnapshot) error {
	for _, previous := range directories {
		current, err := openDirectory(previous.binding.OriginalPath)
		if err != nil {
			return fmt.Errorf("%w: required directory changed during qualification: %v", ErrUnverified, err)
		}
		_ = current.handle.Close()
		if current.binding != previous.binding {
			return fmt.Errorf("%w: workspace or protected storage identity changed; restore isolation and requalify", ErrUnverified)
		}
	}
	return nil
}

func closeDirectories(directories []directorySnapshot) {
	for _, directory := range directories {
		_ = directory.handle.Close()
	}
}

func directoryBindings(directories []directorySnapshot) []directoryBinding {
	bindings := make([]directoryBinding, len(directories))
	for i, directory := range directories {
		bindings[i] = directory.binding
	}
	return bindings
}
