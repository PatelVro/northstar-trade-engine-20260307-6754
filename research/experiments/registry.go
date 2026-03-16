package experiments

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"northstar/buildinfo"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	manifestSchemaVersion = 1
	registrySchemaVersion = 1
)

type FileDigest struct {
	Path      string    `json:"path"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
	ModTime   time.Time `json:"mod_time"`
}

type CodeVersion struct {
	Build              buildinfo.Info `json:"build"`
	WorkspaceRoot      string         `json:"workspace_root"`
	SourceFingerprint  string         `json:"source_fingerprint"`
	SourceFileCount    int            `json:"source_file_count"`
	SourceFilesSample  []string       `json:"source_files_sample"`
	FingerprintCreated time.Time      `json:"fingerprint_created"`
}

type DatasetVersion struct {
	Root             string                 `json:"root"`
	Fingerprint      string                 `json:"fingerprint"`
	FileCount        int                    `json:"file_count"`
	Files            []FileDigest           `json:"files"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	FingerprintBuilt time.Time              `json:"fingerprint_built"`
}

type ResultSnapshot struct {
	Fingerprint      string                 `json:"fingerprint"`
	FileCount        int                    `json:"file_count"`
	Files            []FileDigest           `json:"files"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	FingerprintBuilt time.Time              `json:"fingerprint_built"`
}

type Manifest struct {
	SchemaVersion int                    `json:"schema_version"`
	ExperimentID  string                 `json:"experiment_id"`
	Kind          string                 `json:"kind"`
	CreatedAt     time.Time              `json:"created_at"`
	RunRoot       string                 `json:"run_root"`
	Command       []string               `json:"command"`
	Parameters    map[string]string      `json:"parameters"`
	CodeVersion   CodeVersion            `json:"code_version"`
	Dataset       DatasetVersion         `json:"dataset"`
	Results       ResultSnapshot         `json:"results"`
	Notes         map[string]interface{} `json:"notes,omitempty"`
}

type RegistryEntry struct {
	ExperimentID        string    `json:"experiment_id"`
	Kind                string    `json:"kind"`
	CreatedAt           time.Time `json:"created_at"`
	RunRoot             string    `json:"run_root"`
	ManifestPath        string    `json:"manifest_path"`
	CodeFingerprint     string    `json:"code_fingerprint"`
	DatasetFingerprint  string    `json:"dataset_fingerprint"`
	ResultFingerprint   string    `json:"result_fingerprint"`
	BuildSummary        string    `json:"build_summary"`
	TopProfileSlug      string    `json:"top_profile_slug,omitempty"`
	CompletedProfiles   int       `json:"completed_profiles,omitempty"`
	CredibleProfiles    int       `json:"credible_profiles,omitempty"`
	UsableSymbolCount   int       `json:"usable_symbol_count,omitempty"`
	ConfiguredSymbolCnt int       `json:"configured_symbol_count,omitempty"`
}

type Registry struct {
	SchemaVersion int             `json:"schema_version"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Experiments   []RegistryEntry `json:"experiments"`
}

type RegisterRequest struct {
	ExperimentID    string
	Kind            string
	RunRoot         string
	WorkspaceRoot   string
	RegistryRoot    string
	Command         []string
	Parameters      map[string]string
	DatasetRoot     string
	DatasetFiles    []string
	DatasetMetadata map[string]interface{}
	ResultFiles     []string
	ResultMetadata  map[string]interface{}
	Notes           map[string]interface{}
}

func Register(req RegisterRequest) (*Manifest, error) {
	runRoot, err := filepath.Abs(strings.TrimSpace(req.RunRoot))
	if err != nil {
		return nil, fmt.Errorf("resolve run root: %w", err)
	}
	if strings.TrimSpace(req.ExperimentID) == "" {
		return nil, fmt.Errorf("experiment id is required")
	}
	if strings.TrimSpace(req.Kind) == "" {
		req.Kind = "backtest"
	}

	workspaceRoot := strings.TrimSpace(req.WorkspaceRoot)
	if workspaceRoot == "" {
		if workspaceRoot, err = os.Getwd(); err != nil {
			return nil, fmt.Errorf("resolve workspace root: %w", err)
		}
	}
	workspaceRoot, err = filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}

	registryRoot := strings.TrimSpace(req.RegistryRoot)
	if registryRoot == "" {
		registryRoot = filepath.Join(workspaceRoot, "output", "research", "experiments")
	}
	registryRoot, err = filepath.Abs(registryRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve registry root: %w", err)
	}
	if err := os.MkdirAll(registryRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create registry root: %w", err)
	}

	codeVersion, err := fingerprintCodeVersion(workspaceRoot)
	if err != nil {
		return nil, err
	}
	datasetVersion, err := fingerprintFiles(req.DatasetRoot, req.DatasetFiles, req.DatasetMetadata)
	if err != nil {
		return nil, fmt.Errorf("fingerprint dataset: %w", err)
	}
	resultFingerprint, err := fingerprintFiles(runRoot, req.ResultFiles, req.ResultMetadata)
	if err != nil {
		return nil, fmt.Errorf("fingerprint results: %w", err)
	}
	resultSnapshot := ResultSnapshot{
		Fingerprint:      resultFingerprint.Fingerprint,
		FileCount:        resultFingerprint.FileCount,
		Files:            append([]FileDigest(nil), resultFingerprint.Files...),
		Metadata:         cloneAnyMap(resultFingerprint.Metadata),
		FingerprintBuilt: resultFingerprint.FingerprintBuilt,
	}

	manifest := &Manifest{
		SchemaVersion: manifestSchemaVersion,
		ExperimentID:  req.ExperimentID,
		Kind:          req.Kind,
		CreatedAt:     time.Now().UTC(),
		RunRoot:       runRoot,
		Command:       append([]string(nil), req.Command...),
		Parameters:    cloneStringMap(req.Parameters),
		CodeVersion:   codeVersion,
		Dataset:       datasetVersion,
		Results:       resultSnapshot,
		Notes:         cloneAnyMap(req.Notes),
	}

	manifestPath := filepath.Join(runRoot, "experiment_manifest.json")
	if err := writeJSON(manifestPath, manifest); err != nil {
		return nil, fmt.Errorf("write run manifest: %w", err)
	}
	registryManifestPath := filepath.Join(registryRoot, req.ExperimentID+".json")
	if err := writeJSON(registryManifestPath, manifest); err != nil {
		return nil, fmt.Errorf("write registry manifest: %w", err)
	}

	entry := RegistryEntry{
		ExperimentID:       req.ExperimentID,
		Kind:               req.Kind,
		CreatedAt:          manifest.CreatedAt,
		RunRoot:            runRoot,
		ManifestPath:       registryManifestPath,
		CodeFingerprint:    manifest.CodeVersion.SourceFingerprint,
		DatasetFingerprint: manifest.Dataset.Fingerprint,
		ResultFingerprint:  manifest.Results.Fingerprint,
		BuildSummary:       manifest.CodeVersion.Build.Summary(),
	}
	if slug := stringFromMetadata(req.ResultMetadata, "top_profile_slug"); slug != "" {
		entry.TopProfileSlug = slug
	}
	entry.CompletedProfiles = intFromMetadata(req.ResultMetadata, "completed_profiles")
	entry.CredibleProfiles = intFromMetadata(req.ResultMetadata, "credible_profiles")
	entry.UsableSymbolCount = intFromMetadata(req.DatasetMetadata, "usable_symbol_count")
	entry.ConfiguredSymbolCnt = intFromMetadata(req.DatasetMetadata, "configured_symbol_count")

	registryPath := filepath.Join(registryRoot, "registry.json")
	if err := updateRegistry(registryPath, entry); err != nil {
		return nil, fmt.Errorf("update registry: %w", err)
	}

	return manifest, nil
}

func CollectBacktestResultFiles(runRoot string) ([]string, error) {
	runRoot, err := filepath.Abs(runRoot)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, 32)
	err = filepath.WalkDir(runRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := strings.ToLower(d.Name())
			if name == "decision_logs" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(runRoot, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		switch {
		case strings.HasPrefix(relSlash, "profiles/") && strings.Contains(relSlash, "/output/"):
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".json" || ext == ".csv" {
				files = append(files, path)
			}
		case relSlash == "leaderboard.json" || relSlash == "leaderboard.csv" || relSlash == "study_summary.json" || relSlash == "study_summary.md":
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func DefaultCodeFiles(workspaceRoot string) ([]string, error) {
	workspaceRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, err
	}
	roots := []string{
		"go.mod",
		"go.sum",
		"cmd",
		"trader",
		"risk",
		"market",
		"data",
		"decision",
		"research",
		"config",
		"orders",
		"positions",
		"alerts",
		"audit",
		"broker",
		"buildinfo",
		"logger",
		"manager",
		"news",
		"pool",
		"mcp",
	}

	files := make([]string, 0, 256)
	for _, root := range roots {
		full := filepath.Join(workspaceRoot, root)
		info, err := os.Stat(full)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, full)
			continue
		}
		err = filepath.WalkDir(full, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				name := strings.ToLower(d.Name())
				if name == "node_modules" || name == "output" || name == "runtime" || strings.HasPrefix(name, ".git") {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".go" || ext == ".mod" || ext == ".sum" {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return dedupe(files), nil
}

func fingerprintCodeVersion(workspaceRoot string) (CodeVersion, error) {
	files, err := DefaultCodeFiles(workspaceRoot)
	if err != nil {
		return CodeVersion{}, fmt.Errorf("collect code files: %w", err)
	}
	fp, digests, err := computeFileFingerprint(workspaceRoot, files)
	if err != nil {
		return CodeVersion{}, fmt.Errorf("hash code files: %w", err)
	}
	sample := make([]string, 0, minInt(12, len(digests)))
	for i := 0; i < len(digests) && i < 12; i++ {
		sample = append(sample, digests[i].Path)
	}
	return CodeVersion{
		Build:              buildinfo.Current(),
		WorkspaceRoot:      workspaceRoot,
		SourceFingerprint:  fp,
		SourceFileCount:    len(digests),
		SourceFilesSample:  sample,
		FingerprintCreated: time.Now().UTC(),
	}, nil
}

func fingerprintFiles(root string, files []string, metadata map[string]interface{}) (DatasetVersion, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return DatasetVersion{}, err
	}
	fp, digests, err := computeFileFingerprint(root, files)
	if err != nil {
		return DatasetVersion{}, err
	}
	return DatasetVersion{
		Root:             root,
		Fingerprint:      fp,
		FileCount:        len(digests),
		Files:            digests,
		Metadata:         cloneAnyMap(metadata),
		FingerprintBuilt: time.Now().UTC(),
	}, nil
}

func computeFileFingerprint(root string, files []string) (string, []FileDigest, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", nil, err
	}
	normalized := dedupe(files)
	sort.Strings(normalized)

	manifestHash := sha256.New()
	digests := make([]FileDigest, 0, len(normalized))
	for _, file := range normalized {
		if strings.TrimSpace(file) == "" {
			continue
		}
		absPath, err := filepath.Abs(file)
		if err != nil {
			return "", nil, err
		}
		digest, err := hashSingleFile(root, absPath)
		if err != nil {
			return "", nil, err
		}
		digests = append(digests, digest)
		io.WriteString(manifestHash, digest.Path)
		io.WriteString(manifestHash, "\n")
		io.WriteString(manifestHash, digest.SHA256)
		io.WriteString(manifestHash, "\n")
	}
	return hex.EncodeToString(manifestHash.Sum(nil)), digests, nil
}

func hashSingleFile(root, path string) (FileDigest, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileDigest{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return FileDigest{}, err
	}

	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return FileDigest{}, err
	}

	rel := path
	if root != "" {
		if candidate, err := filepath.Rel(root, path); err == nil {
			rel = candidate
		}
	}

	return FileDigest{
		Path:      filepath.ToSlash(rel),
		SHA256:    hex.EncodeToString(sum.Sum(nil)),
		SizeBytes: info.Size(),
		ModTime:   info.ModTime().UTC(),
	}, nil
}

func updateRegistry(path string, entry RegistryEntry) error {
	registry := Registry{
		SchemaVersion: registrySchemaVersion,
		Experiments:   []RegistryEntry{},
	}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &registry); err != nil {
			return err
		}
	}

	replaced := false
	for i := range registry.Experiments {
		if registry.Experiments[i].ExperimentID == entry.ExperimentID {
			registry.Experiments[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		registry.Experiments = append(registry.Experiments, entry)
	}
	sort.Slice(registry.Experiments, func(i, j int) bool {
		if registry.Experiments[i].CreatedAt.Equal(registry.Experiments[j].CreatedAt) {
			return registry.Experiments[i].ExperimentID > registry.Experiments[j].ExperimentID
		}
		return registry.Experiments[i].CreatedAt.After(registry.Experiments[j].CreatedAt)
	})
	registry.UpdatedAt = time.Now().UTC()
	registry.SchemaVersion = registrySchemaVersion

	return writeJSON(path, registry)
}

func writeJSON(path string, value interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneAnyMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringFromMetadata(m map[string]interface{}, key string) string {
	if len(m) == 0 {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func intFromMetadata(m map[string]interface{}, key string) int {
	if len(m) == 0 {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func dedupe(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
