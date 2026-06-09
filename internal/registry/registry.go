// Package registry builds the machine-readable index that the kasas dashboard
// consumes to list and install community plugins.
//
// The index is a single deterministic JSON document generated from the plugins/
// directory. For every plugin that passes the submission gate it records the
// manifest metadata, the capability tier, and — critically for install integrity —
// a per-file SHA-256 and an aggregate content hash. The dashboard fetches the
// index, shows the user what a plugin does and what it can touch, downloads the
// listed files, and verifies each hash before writing them into plugins.dir. The
// hashes are the chain of custody between "reviewed in this repo" and "running on a
// user's machine".
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/paulmeier/kasas-plugins/internal/gate"
	"github.com/paulmeier/kasas-plugins/internal/manifest"
)

// SchemaVersion is the version of the index document format. The dashboard checks
// it before parsing so the format can evolve without breaking older clients.
const SchemaVersion = 1

// Index is the top-level registry document, written to registry/index.json.
type Index struct {
	SchemaVersion int       `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	Repository    string    `json:"repository"`
	Plugins       []Plugin  `json:"plugins"`
}

// Plugin is one listed plugin. The fields mirror the manifest plus the
// registry-computed integrity and provenance data.
type Plugin struct {
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Description    string   `json:"description"`
	Author         string   `json:"author"`
	License        string   `json:"license"`
	Homepage       string   `json:"homepage"`
	Runtime        string   `json:"runtime"`
	Entrypoint     string   `json:"entrypoint"`
	Hooks          []string `json:"hooks"`
	Capabilities   []string `json:"capabilities"`
	CapabilityTier string   `json:"capability_tier"`
	Path           string   `json:"path"`         // repo-relative dir, e.g. "plugins/budgeting"
	Files          []File   `json:"files"`        // every file the installer must fetch
	ContentHash    string   `json:"content_hash"` // "sha256:..." over all files
	SizeBytes      int64    `json:"size_bytes"`
}

// File is one installable file with its integrity hash, so the dashboard can verify
// each downloaded byte before it lands in plugins.dir.
type File struct {
	Path   string `json:"path"`   // plugin-relative, forward slashes
	SHA256 string `json:"sha256"` // hex of the file's SHA-256
	Size   int64  `json:"size"`
}

// Build scans pluginsDir (e.g. "<repo>/plugins"), gates every plugin, and returns
// an Index containing only the plugins that pass. The returned error is non-nil
// only on an I/O failure; a plugin that fails the gate is reported via failures so
// the caller can decide whether that is fatal (it is, in CI).
func Build(repoURL, pluginsDir, repoRelPluginsDir string, limits gate.Limits) (*Index, []*gate.Report, error) {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &Index{SchemaVersion: SchemaVersion, Repository: repoURL, Plugins: []Plugin{}}, nil, nil
		}
		return nil, nil, err
	}

	idx := &Index{SchemaVersion: SchemaVersion, Repository: repoURL, Plugins: []Plugin{}}
	var failures []*gate.Report

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(pluginsDir, e.Name())
		rep := gate.CheckPlugin(dir, limits)
		if !rep.OK() {
			failures = append(failures, rep)
			continue
		}
		p, perr := pluginEntry(rep, dir, filepath.ToSlash(filepath.Join(repoRelPluginsDir, e.Name())))
		if perr != nil {
			return nil, nil, fmt.Errorf("index %s: %w", e.Name(), perr)
		}
		idx.Plugins = append(idx.Plugins, p)
	}

	// Deterministic ordering so the committed index is stable across machines.
	sort.Slice(idx.Plugins, func(i, j int) bool { return idx.Plugins[i].Name < idx.Plugins[j].Name })
	return idx, failures, nil
}

func pluginEntry(rep *gate.Report, dir, repoRelPath string) (Plugin, error) {
	m := rep.Manifest
	files, total, hash, err := hashTree(dir)
	if err != nil {
		return Plugin{}, err
	}
	return Plugin{
		Name:           m.Name,
		Version:        m.Version,
		Description:    m.Description,
		Author:         m.Author,
		License:        m.License,
		Homepage:       m.Homepage,
		Runtime:        m.Runtime,
		Entrypoint:     m.Entrypoint,
		Hooks:          hookStrings(m.Hooks),
		Capabilities:   capStrings(m.Capabilities),
		CapabilityTier: string(rep.CapabilityTier),
		Path:           repoRelPath,
		Files:          files,
		ContentHash:    hash,
		SizeBytes:      total,
	}, nil
}

// hashTree returns the per-file hashes (sorted by path), the total size, and an
// aggregate content hash. The aggregate is the SHA-256 of "path\x00filehash\n"
// lines in path order, so it is independent of filesystem ordering and changes if
// any file's name or content changes.
func hashTree(dir string) ([]File, int64, string, error) {
	var files []File
	var total int64
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(dir, path)
		sum := sha256.Sum256(data)
		files = append(files, File{
			Path:   filepath.ToSlash(rel),
			SHA256: hex.EncodeToString(sum[:]),
			Size:   int64(len(data)),
		})
		total += int64(len(data))
		return nil
	})
	if err != nil {
		return nil, 0, "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	agg := sha256.New()
	for _, f := range files {
		fmt.Fprintf(agg, "%s\x00%s\n", f.Path, f.SHA256)
	}
	return files, total, "sha256:" + hex.EncodeToString(agg.Sum(nil)), nil
}

// Marshal renders the index as stable, human-diffable JSON (2-space indent,
// trailing newline). A stable encoding is what lets CI enforce that the committed
// index matches the plugins directory.
func (idx *Index) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func hookStrings(hs []manifest.Hook) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = string(h)
	}
	return out
}

func capStrings(cs []manifest.Capability) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = string(c)
	}
	return out
}
