// Package workspace captures independent working-file copies and binds filesystem
// ownership checks to actual directory identities. These checks do not enforce a
// sandbox or authorize a runtime launch. The trusted coordinator must retain the
// snapshot outside worker-writable storage and enforce execution permissions.
package workspace

import "errors"

var (
	// ErrUnsafe reports unsafe paths or file types in a workspace capture.
	ErrUnsafe = errors.New("unsafe swarm workspace")
	// ErrChanged reports changed filesystem identities or captured contents.
	ErrChanged = errors.New("swarm workspace identity or contents changed")
	// ErrUnsupported reports filesystem features without a supported capture implementation.
	ErrUnsupported = errors.New("unsupported swarm filesystem")
)

// CaptureRequest selects a source tree and an exclusive destination for an independent copy.
type CaptureRequest struct {
	SourceRoot  string
	Destination string
	// IncludeIgnored explicitly selects additional repository-relative files or
	// directories. No ignored inputs are included by default. Git metadata is
	// never copied, even if explicitly requested.
	IncludeIgnored []string
}

// RootIdentity binds a requested root to its canonical path and filesystem identity.
type RootIdentity struct {
	RequestedPath string `json:"requestedPath"`
	Path          string `json:"path"`
	FileID        string `json:"fileId"`
}

// Entry records a working-tree path, metadata and content identity or explicit absence.
type Entry struct {
	Path       string `json:"path"`
	FileID     string `json:"fileId,omitempty"`
	Mode       uint32 `json:"mode,omitempty"`
	Size       int64  `json:"size,omitempty"`
	ModifiedNS int64  `json:"modifiedNs,omitempty"`
	ChangedNS  int64  `json:"changedNs,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Missing    bool   `json:"missing,omitempty"`
	Directory  bool   `json:"directory,omitempty"`
}

// IndexEntry records a Git index path, stage, mode and object identity.
type IndexEntry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Object string `json:"object"`
	Stage  string `json:"stage"`
}

// Manifest freezes working-file and Git-index facts for preservation checks.
type Manifest struct {
	Root               RootIdentity `json:"root"`
	GitDirectory       RootIdentity `json:"gitDirectory"`
	GitCommonDirectory RootIdentity `json:"gitCommonDirectory"`
	Head               string       `json:"head"`
	HeadRef            string       `json:"headRef"`
	IndexPath          string       `json:"indexPath"`
	Index              Entry        `json:"index"`
	// IndexBytes preserve staging exactly without handing worker code access to
	// the source index. JSON serializes these bytes as base64. Recovery of staged
	// objects still requires the original Git object store; object/history backup
	// is not provided by this working-file snapshot.
	IndexBytes     []byte       `json:"indexBytes"`
	IndexEntries   []IndexEntry `json:"indexEntries"`
	IncludeIgnored []string     `json:"includeIgnored"`
	Entries        []Entry      `json:"entries"`
}

// Snapshot binds the source manifest to the independent destination identity.
type Snapshot struct {
	Source       Manifest     `json:"source"`
	Destination  RootIdentity `json:"destination"`
	ManifestHash string       `json:"manifestHash"`
	IdentityHash string       `json:"identityHash"`
}

// Submission binds candidate file contents to the captured workspace snapshot.
type Submission struct {
	SnapshotHash string       `json:"snapshotHash"`
	Root         RootIdentity `json:"root"`
	Entries      []Entry      `json:"entries"`
	Hash         string       `json:"hash"`
}
