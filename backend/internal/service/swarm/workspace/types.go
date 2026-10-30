// Package workspace captures independent working-file copies and binds filesystem
// ownership checks to actual directory identities. These checks do not enforce a
// sandbox or authorize a runtime launch. The trusted coordinator must retain the
// snapshot outside worker-writable storage and enforce execution permissions.
package workspace

import "errors"

var (
	ErrUnsafe      = errors.New("unsafe swarm workspace")
	ErrChanged     = errors.New("swarm workspace identity or contents changed")
	ErrUnsupported = errors.New("unsupported swarm filesystem")
)

type CaptureRequest struct {
	SourceRoot  string
	Destination string
	// IncludeIgnored explicitly selects additional repository-relative files or
	// directories. No ignored inputs are included by default. Git metadata is
	// never copied, even if explicitly requested.
	IncludeIgnored []string
}

type RootIdentity struct {
	RequestedPath string `json:"requestedPath"`
	Path          string `json:"path"`
	FileID        string `json:"fileId"`
}

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

type IndexEntry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Object string `json:"object"`
	Stage  string `json:"stage"`
}

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

type Snapshot struct {
	Source       Manifest     `json:"source"`
	Destination  RootIdentity `json:"destination"`
	ManifestHash string       `json:"manifestHash"`
	IdentityHash string       `json:"identityHash"`
}

type Submission struct {
	SnapshotHash string       `json:"snapshotHash"`
	Root         RootIdentity `json:"root"`
	Entries      []Entry      `json:"entries"`
	Hash         string       `json:"hash"`
}
