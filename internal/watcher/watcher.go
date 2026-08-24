package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/pathfilter"
	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
	"github.com/Athernaa/code-scale-mcpv2/internal/security"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/summarizer"
	"github.com/Athernaa/code-scale-mcpv2/internal/workspace"
	workspaceindex "github.com/Athernaa/code-scale-mcpv2/internal/workspace/indexer"
	"github.com/fsnotify/fsnotify"
)

const debounceInterval = 500 * time.Millisecond

// FolderWatch tracks a single watched folder. Persisted watches intentionally
// retain only the path and repository. The watcher reloads repository
// .gitignore rules, but per-call extra ignore patterns are not persisted
// across process restarts.
type FolderWatch struct {
	Path      string    `json:"path"`
	Repo      string    `json:"repo"`
	StartedAt time.Time `json:"started_at"`
	watcher   *fsnotify.Watcher
	stop      chan struct{}
}

// Manager manages file watchers for indexed folders.
type Manager struct {
	mu      sync.Mutex
	watches map[string]*FolderWatch // key: absolute path
	store   *storage.IndexStore
}

// NewManager creates a new watcher manager.
func NewManager(store *storage.IndexStore) *Manager {
	return &Manager{
		watches: make(map[string]*FolderWatch),
		store:   store,
	}
}

// Watch starts watching a folder for changes and auto-reindexes.
func (m *Manager) Watch(absPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	localID, err := repository.Local(absPath)
	if err != nil {
		return err
	}
	absPath = localID.CanonicalPath

	if _, exists := m.watches[absPath]; exists {
		return nil // Already watching
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Add all subdirectories recursively. The same helper is used when a
	// directory appears later so nested directories are never missed.
	err = addDirectoryRecursive(w, absPath, absPath)
	if err != nil {
		_ = w.Close()
		return err
	}

	repo := localID.Repo

	fw := &FolderWatch{
		Path:      absPath,
		Repo:      repo,
		StartedAt: time.Now(),
		watcher:   w,
		stop:      make(chan struct{}),
	}
	m.watches[absPath] = fw

	// Persist watch to database for restore on restart
	if err := m.store.SaveWatch(absPath, repo); err != nil {
		log.Printf("watcher: failed to persist watch for %s: %v", absPath, err)
	}

	go m.watchLoop(fw)

	return nil
}

// addDirectoryRecursive registers path and all eligible descendants. Symlink
// directories are skipped to avoid loops and escapes outside the watched root.
func addDirectoryRecursive(w *fsnotify.Watcher, root, path string) error {
	if !security.ValidatePath(root, path) || security.IsSymlinkEscape(root, path) {
		return fmt.Errorf("directory is outside watched root: %s", path)
	}

	var firstErr error
	err := filepath.WalkDir(path, func(current string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("watcher: cannot inspect %s: %v", current, walkErr)
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if current != root && security.ShouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		if !security.ValidatePath(root, current) {
			return filepath.SkipDir
		}
		if err := w.Add(current); err != nil {
			log.Printf("watcher: failed to watch %s: %v", current, err)
			if current == path && firstErr == nil {
				firstErr = err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return firstErr
}

// Unwatch stops watching a folder.
func (m *Manager) Unwatch(absPath string) error {
	if canonical, err := repository.CanonicalPath(absPath); err == nil {
		absPath = canonical
	}
	m.mu.Lock()
	fw, exists := m.watches[absPath]
	if !exists {
		m.mu.Unlock()
		return nil
	}
	delete(m.watches, absPath)

	// Close stop channel and watcher while still holding the lock
	// to prevent Watch from racing on the same path.
	close(fw.stop)
	err := fw.watcher.Close()
	m.mu.Unlock()

	// Remove from database (safe outside lock)
	if dbErr := m.store.DeleteWatch(absPath); dbErr != nil {
		log.Printf("watcher: failed to delete persisted watch for %s: %v", absPath, dbErr)
	}

	return err
}

// ListWatches returns all active watches.
func (m *Manager) ListWatches() []FolderWatch {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]FolderWatch, 0, len(m.watches))
	for _, fw := range m.watches {
		result = append(result, FolderWatch{
			Path:      fw.Path,
			Repo:      fw.Repo,
			StartedAt: fw.StartedAt,
		})
	}
	return result
}

// RestoreWatches restores persisted watches from the database.
func (m *Manager) RestoreWatches() error {
	saved, err := m.store.ListSavedWatches()
	if err != nil {
		return fmt.Errorf("list saved watches: %w", err)
	}

	for _, sw := range saved {
		// Check if the directory still exists
		info, err := os.Stat(sw.Path)
		if err != nil || !info.IsDir() {
			log.Printf("watcher: removing stale watch for %s (no longer exists)", sw.Path)
			_ = m.store.DeleteWatch(sw.Path)
			continue
		}
		localID, identityErr := repository.Local(sw.Path)
		if identityErr != nil || localID.Repo != sw.Repo {
			log.Printf("watcher: removing stale watch for %s (repository identity changed)", sw.Path)
			_ = m.store.DeleteWatch(sw.Path)
			continue
		}

		if err := m.Watch(sw.Path); err != nil {
			log.Printf("watcher: failed to restore watch for %s: %v", sw.Path, err)
			continue
		}
		log.Printf("watcher: restored watch for %s", sw.Path)
	}
	return nil
}

// Close stops all watchers.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for path, fw := range m.watches {
		close(fw.stop)
		_ = fw.watcher.Close()
		delete(m.watches, path)
	}
}

// watchLoop handles fsnotify events with debouncing.
func (m *Manager) watchLoop(fw *FolderWatch) {
	var pendingMu sync.Mutex
	pending := make(map[string]struct{})
	var debounceTimer *time.Timer

	for {
		select {
		case <-fw.stop:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}

			// Only care about create/write/remove of supported files
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}

			// If a new directory was created or moved in, watch it recursively.
			if event.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if !security.ShouldSkipDir(filepath.Base(event.Name)) && security.ValidatePath(fw.Path, event.Name) && !security.IsSymlinkEscape(fw.Path, event.Name) {
						if err := addDirectoryRecursive(fw.watcher, fw.Path, event.Name); err != nil {
							log.Printf("watcher: failed to add directory %s: %v", event.Name, err)
						}
						// A populated directory can arrive in one rename/create
						// event. Watching it is not enough; enqueue its existing
						// supported files for indexing as well.
						if mode, _ := workspace.DetectMode(fw.Path); mode == workspace.KindFiveMWorkspace {
							if matcher, matcherErr := pathfilter.New(fw.Path, nil); matcherErr == nil {
								if paths := discoverSupportedFiles(fw.Path, event.Name, matcher); len(paths) > 0 {
									m.reindexFiles(fw, paths)
								}
							}
						}
					}
					continue
				}
			}

			// Workspace configuration is indexed as metadata, not as source.
			lang := parser.DetectLanguage(event.Name)
			if lang == "" && !strings.EqualFold(filepath.Ext(event.Name), ".cfg") {
				if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					if err := m.handleRemovedDirectory(fw.Path, fw.Repo, event.Name); err != nil {
						log.Printf("watcher: directory removal refresh failed: %v", err)
					}
				}
				continue
			}

			// Security filter
			if security.ShouldSkipFile(filepath.Base(event.Name)) {
				continue
			}

			pendingMu.Lock()
			pending[event.Name] = struct{}{}
			pendingMu.Unlock()

			// Reset debounce timer
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounceInterval, func() {
				// Check if watcher was stopped before processing
				select {
				case <-fw.stop:
					return
				default:
				}

				pendingMu.Lock()
				paths := make([]string, 0, len(pending))
				for p := range pending {
					paths = append(paths, p)
				}
				pending = make(map[string]struct{})
				pendingMu.Unlock()

				if len(paths) > 0 {
					m.reindexFiles(fw, paths)
				}
			})

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error for %s: %v", fw.Path, err)
		}
	}
}

func (m *Manager) handleRemovedDirectory(root, repo, removedPath string) error {
	local, err := repository.Local(root)
	if err != nil {
		return err
	}
	repoID, repoErr := m.store.GetRepoID(repo)
	previousWorkspace := false
	if repoErr == nil {
		if previous, workspaceErr := m.store.GetWorkspace(repoID); workspaceErr == nil && previous.Kind == workspace.KindFiveMWorkspace {
			previousWorkspace = true
		}
	}
	currentMode, _ := workspace.DetectMode(root)
	if matcher, matcherErr := pathfilter.New(root, nil); matcherErr == nil {
		if currentDiscovery, discoveryErr := workspace.DiscoverWithIgnore(root, matcher.Ignored); discoveryErr == nil {
			currentMode = currentDiscovery.Mode
		}
	}
	if !previousWorkspace && currentMode != workspace.KindFiveMWorkspace {
		return nil
	}
	rel, err := filepath.Rel(root, removedPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("removed directory is outside workspace: %s", removedPath)
	}
	if err := m.store.DeleteFilesUnderPrefix(local.Owner, local.Name, filepath.ToSlash(rel)); err != nil {
		return err
	}
	if repoErr != nil {
		return repoErr
	}
	if currentMode == workspace.KindFiveMWorkspace {
		return m.rebuildWorkspace(root, repo)
	}
	return m.clearWorkspaceMode(repoID)
}

// reindexFiles re-parses and updates the index for changed files.
func (m *Manager) reindexFiles(fw *FolderWatch, paths []string) {
	localID, err := repository.Local(fw.Path)
	if err != nil {
		log.Printf("watcher: cannot resolve repository identity for %s: %v", fw.Path, err)
		return
	}
	owner := localID.Owner
	repoName := localID.Name
	resourceName, err := repository.LocalResourceName(localID.CanonicalPath)
	if err != nil {
		log.Printf("watcher: cannot derive resource name for %s: %v", fw.Path, err)
		return
	}
	workspaceDirty := false
	workspaceTopologyDirty := false
	workspaceConfigDirty := false
	workspaceSourceDirty := false
	workspaceResources := map[string]bool{}
	matcher, _ := pathfilter.New(fw.Path, nil)
	workspaceDiscovery, _ := workspace.Discover(fw.Path)
	if matcher != nil {
		workspaceDiscovery, _ = workspace.DiscoverWithIgnore(fw.Path, matcher.Ignored)
	}
	mode := workspaceDiscovery.Mode
	indexedRepoID, indexedRepoErr := m.store.GetRepoID(owner + "/" + repoName)
	previousWorkspace := false
	if indexedRepoErr == nil {
		if previous, workspaceErr := m.store.GetWorkspace(indexedRepoID); workspaceErr == nil && previous.Kind == workspace.KindFiveMWorkspace {
			previousWorkspace = true
		}
	}
	workspaceActive := previousWorkspace || mode == workspace.KindFiveMWorkspace
	workspaceModeTransitioned := previousWorkspace && mode != workspace.KindFiveMWorkspace
	if workspaceModeTransitioned {
		workspaceDirty = true
		workspaceTopologyDirty = true
	}
	ignoreChanged := false
	for _, path := range paths {
		if strings.EqualFold(filepath.Base(path), ".gitignore") {
			ignoreChanged = true
			break
		}
	}
	if ignoreChanged && matcher != nil && indexedRepoErr == nil {
		if !previousWorkspace && mode == workspace.KindFiveMWorkspace {
			workspaceDirty = true
			workspaceTopologyDirty = true
		}
		indexed := map[string]bool{}
		if indexedFiles, filesErr := m.store.GetFiles(indexedRepoID); filesErr == nil {
			for _, file := range indexedFiles {
				indexed[workspace.NormalizePath(file.Path)] = true
				if matcher.Ignored(filepath.Join(fw.Path, filepath.FromSlash(file.Path)), false) {
					paths = appendUniquePath(paths, filepath.Join(fw.Path, filepath.FromSlash(file.Path)))
				}
			}
		}
		for _, path := range discoverSupportedFiles(fw.Path, fw.Path, matcher) {
			rel, relErr := filepath.Rel(fw.Path, path)
			if relErr == nil && !indexed[workspace.NormalizePath(filepath.ToSlash(rel))] {
				paths = appendUniquePath(paths, path)
			}
		}
	}

	for _, fullPath := range paths {
		relPath, err := filepath.Rel(fw.Path, fullPath)
		if err != nil {
			continue
		}
		relPath = filepath.ToSlash(relPath)

		// Check if file was deleted
		_, statErr := os.Stat(fullPath)
		ignoredIndexedFile := matcher != nil && matcher.Ignored(fullPath, false)
		if os.IsNotExist(statErr) || ignoredIndexedFile {
			log.Printf("watcher: file removed %s, cleaning index", relPath)
			deleteErr := m.store.DeleteFileFromIndex(owner, repoName, relPath)
			if deleteErr != nil {
				log.Printf("watcher: failed to remove %s from index: %v", relPath, deleteErr)
			}
			if indexedRepoErr != nil {
				log.Printf("watcher: cannot refresh semantic graph after removing %s: %v", relPath, indexedRepoErr)
			} else if workspaceActive {
				workspaceDirty = true
				workspaceSourceDirty = true
				if isManifestPath(relPath) {
					workspaceTopologyDirty = true
				} else if resource, ok := workspace.ResourceForPath(workspaceDiscovery.Resources, relPath); ok {
					workspaceResources[resource.RelativePath] = true
				}
				_ = m.store.ReplaceSemanticFileForAnalyzer(indexedRepoID, semantic.AnalyzerGenericGraph, relPath, nil)
				if graphErr := m.refreshGenericRelationships(owner + "/" + repoName); graphErr != nil {
					log.Printf("watcher: failed to refresh generic relationships: %v", graphErr)
				}
			} else if deleteErr == nil && isManifestPath(relPath) {
				_ = m.store.ReplaceSemanticFileForAnalyzer(indexedRepoID, semantic.AnalyzerGenericGraph, relPath, nil)
				if rebuildErr := m.rebuildSemanticRepository(indexedRepoID, owner+"/"+repoName, resourceName); rebuildErr != nil {
					log.Printf("watcher: failed to clear FiveM semantics after removing %s: %v", relPath, rebuildErr)
				}
			} else if deleteErr == nil {
				if graphErr := m.store.ReplaceSemanticFileForAnalyzer(indexedRepoID, semantic.AnalyzerFiveM, relPath, nil); graphErr != nil {
					log.Printf("watcher: failed to remove FiveM semantics for %s: %v", relPath, graphErr)
				}
				if graphErr := m.store.ReplaceSemanticFileForAnalyzer(indexedRepoID, semantic.AnalyzerGenericGraph, relPath, nil); graphErr != nil {
					log.Printf("watcher: failed to remove generic graph facts for %s: %v", relPath, graphErr)
				}
				if graphErr := m.refreshSemanticRelationships(owner + "/" + repoName); graphErr != nil {
					log.Printf("watcher: failed to refresh FiveM relationships: %v", graphErr)
				}
				if graphErr := m.refreshGenericRelationships(owner + "/" + repoName); graphErr != nil {
					log.Printf("watcher: failed to refresh generic relationships: %v", graphErr)
				}
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(relPath), ".cfg") {
			if workspaceActive {
				workspaceDirty = true
				workspaceConfigDirty = true
			}
			continue
		}
		if strings.EqualFold(filepath.Base(relPath), ".gitignore") {
			continue
		}
		if matcher != nil && matcher.Ignored(fullPath, false) {
			continue
		}

		// Security: check for symlink escape and path traversal
		if security.IsSymlinkEscape(fw.Path, fullPath) {
			continue
		}
		if reason := security.ShouldExcludeFile(fullPath, fw.Path, security.DefaultMaxFileSize); reason != "" {
			continue
		}

		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		if security.IsBinaryContent(content) {
			continue
		}

		lang := parser.DetectLanguage(relPath)
		symbols, err := parser.ParseFile(content, relPath, lang)
		if err != nil {
			log.Printf("watcher: parse error for %s: %v", relPath, err)
			continue
		}

		summarizer.SummarizeSymbols(symbols, false)

		h := sha256.Sum256(content)
		hash := hex.EncodeToString(h[:])

		// Update the cache and only this file's index together; a full replacement
		// would erase unrelated files and a split cache/index update can leave
		// stored byte offsets pointing at the wrong source bytes.
		err = m.store.UpsertFileIndexWithContent(owner, repoName, "local", "", relPath, hash, lang, symbols, content, localID.CanonicalPath)
		if err != nil {
			log.Printf("watcher: save error for %s: %v", relPath, err)
			continue
		}

		if mode == workspace.KindFiveMWorkspace {
			workspaceDirty = true
			workspaceSourceDirty = true
			if isManifestPath(relPath) {
				workspaceTopologyDirty = true
			} else if resource, ok := workspace.ResourceForPath(workspaceDiscovery.Resources, relPath); ok {
				workspaceResources[resource.RelativePath] = true
			}
		} else if err := m.updateSemanticFile(owner+"/"+repoName, resourceName, relPath, lang, content, symbols); err != nil {
			log.Printf("watcher: semantic update failed for %s: %v", relPath, err)
		}
		if err := m.updateGenericFile(owner+"/"+repoName, relPath, lang, content, symbols); err != nil {
			log.Printf("watcher: generic graph update failed for %s: %v", relPath, err)
		}

		log.Printf("watcher: reindexed %s (%d symbols)", relPath, len(symbols))
	}
	if workspaceDirty {
		if workspaceTopologyDirty {
			if workspaceModeTransitioned {
				if err := m.clearWorkspaceMode(indexedRepoID); err != nil {
					log.Printf("watcher: workspace mode cleanup failed: %v", err)
				}
			} else if err := m.rebuildWorkspace(localID.CanonicalPath, owner+"/"+repoName); err != nil {
				log.Printf("watcher: workspace refresh failed: %v", err)
			}
		} else {
			for resourcePath := range workspaceResources {
				if err := m.refreshWorkspaceResource(localID.CanonicalPath, owner+"/"+repoName, resourcePath); err != nil {
					log.Printf("watcher: resource refresh failed for %s: %v", resourcePath, err)
				}
			}
			if workspaceConfigDirty {
				if err := m.refreshWorkspaceConfiguration(localID.CanonicalPath, owner+"/"+repoName); err != nil {
					log.Printf("watcher: workspace configuration refresh failed: %v", err)
				}
			}
			if workspaceSourceDirty && len(workspaceResources) == 0 && !workspaceTopologyDirty {
				if repoID, repoErr := m.store.GetRepoID(owner + "/" + repoName); repoErr == nil {
					if coverageErr := m.updateWorkspaceCoverage(localID.CanonicalPath, repoID); coverageErr != nil {
						log.Printf("watcher: workspace coverage refresh failed: %v", coverageErr)
					}
				}
			}
		}
	}
}

func appendUniquePath(paths []string, path string) []string {
	for _, existing := range paths {
		if filepath.Clean(existing) == filepath.Clean(path) {
			return paths
		}
	}
	return append(paths, path)
}

func (m *Manager) clearWorkspaceMode(repoID int64) error {
	if err := m.store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{}); err != nil {
		return err
	}
	if err := m.store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace, semantic.Result{}); err != nil {
		return err
	}
	return m.store.ClearWorkspaceState(repoID)
}

func discoverSupportedFiles(root, directory string, matchers ...*pathfilter.Matcher) []string {
	var matcher *pathfilter.Matcher
	if len(matchers) > 0 {
		matcher = matchers[0]
	}
	var paths []string
	_ = filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != directory && security.ShouldSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			if matcher != nil && matcher.Ignored(path, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if matcher != nil && matcher.Ignored(path, false) {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || security.ShouldSkipFile(entry.Name()) || security.ShouldExcludeFile(path, root, security.DefaultMaxFileSize) != "" {
			return nil
		}
		if parser.DetectLanguage(entry.Name()) == "" {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr == nil && !strings.HasPrefix(rel, "..") {
			paths = append(paths, filepath.Join(root, rel))
		}
		return nil
	})
	return paths
}

func isManifestPath(path string) bool {
	name := strings.ToLower(filepath.Base(filepath.FromSlash(path)))
	return name == "fxmanifest.lua" || name == "__resource.lua"
}

func (m *Manager) refreshWorkspaceResource(root, repo, resourcePath string) error {
	repoID, err := m.store.GetRepoID(repo)
	if err != nil {
		return err
	}
	files, err := m.store.GetFiles(repoID)
	if err != nil {
		return err
	}
	contents := map[string][]byte{}
	languages := map[string]string{}
	symbols := map[string][]parser.Symbol{}
	for _, file := range files {
		if file.Path != resourcePath && !strings.HasPrefix(file.Path, resourcePath+"/") {
			continue
		}
		content, contentErr := m.store.GetFileContent(repoID, file.Path)
		if contentErr != nil {
			continue
		}
		contents[file.Path] = content
		languages[file.Path] = file.Language
		symbols[file.Path], _ = m.store.GetSymbolsByFile(repoID, file.Path)
	}
	discovery, discoveryErr := workspace.Discover(root)
	if matcher, matcherErr := pathfilter.New(root, nil); matcherErr == nil {
		discovery, discoveryErr = workspace.DiscoverWithIgnore(root, matcher.Ignored)
	}
	if discoveryErr != nil {
		return discoveryErr
	}
	_, err = workspaceindex.RefreshResource(context.Background(), m.store, repoID, repo, root, resourcePath, contents, languages, symbols, discovery)
	if err != nil {
		return err
	}
	return m.updateWorkspaceCoverage(root, repoID)
}

func (m *Manager) refreshWorkspaceConfiguration(root, repo string) error {
	repoID, err := m.store.GetRepoID(repo)
	if err != nil {
		return err
	}
	discovery, err := workspace.Discover(root)
	if matcher, matcherErr := pathfilter.New(root, nil); matcherErr == nil {
		discovery, err = workspace.DiscoverWithIgnore(root, matcher.Ignored)
	}
	if err != nil {
		return err
	}
	_, err = workspaceindex.RefreshWorkspaceConfiguration(m.store, repoID, repo, root, discovery)
	return err
}

func (m *Manager) rebuildWorkspace(root, repo string) error {
	repoID, err := m.store.GetRepoID(repo)
	if err != nil {
		return err
	}
	files, err := m.store.GetFiles(repoID)
	if err != nil {
		return err
	}
	contents := map[string][]byte{}
	langs := map[string]string{}
	symbols := map[string][]parser.Symbol{}
	for _, f := range files {
		data, e := m.store.GetFileContent(repoID, f.Path)
		if e != nil {
			continue
		}
		contents[f.Path] = data
		langs[f.Path] = f.Language
		symbols[f.Path], _ = m.store.GetSymbolsByFile(repoID, f.Path)
	}
	matcher, _ := pathfilter.New(root, nil)
	discovery, discoveryErr := workspace.Discover(root)
	if matcher != nil {
		discovery, discoveryErr = workspace.DiscoverWithIgnore(root, matcher.Ignored)
	}
	if discoveryErr != nil {
		return discoveryErr
	}
	_, err = workspaceindex.Index(context.Background(), m.store, repoID, repo, root, contents, langs, symbols, discovery)
	if err != nil {
		return err
	}
	if err := m.updateWorkspaceCoverage(root, repoID); err != nil {
		return err
	}
	return nil
}

// updateWorkspaceCoverage compares the authoritative filesystem discovery
// with indexed files without reparsing source. Existing incompleteness is
// preserved until a full index proves it can be cleared.
func (m *Manager) updateWorkspaceCoverage(root string, repoID int64) error {
	matcher, _ := pathfilter.New(root, nil)
	discoveredPaths := discoverSupportedFiles(root, root, matcher)
	discovered := make(map[string]bool, len(discoveredPaths))
	for _, path := range discoveredPaths {
		rel, err := filepath.Rel(root, path)
		if err == nil {
			discovered[filepath.ToSlash(rel)] = true
		}
	}
	files, err := m.store.GetFiles(repoID)
	if err != nil {
		return err
	}
	indexed := make(map[string]bool, len(files))
	for _, file := range files {
		indexed[workspace.NormalizePath(file.Path)] = true
	}
	completeCoverage := len(discovered) == len(indexed)
	if completeCoverage {
		for path := range discovered {
			if !indexed[path] {
				completeCoverage = false
				break
			}
		}
	}
	previous, err := m.store.GetWorkspace(repoID)
	if err != nil {
		if storage.IsNotFound(err) {
			return nil
		}
		return err
	}
	return m.store.UpdateWorkspaceCompleteness(repoID, storage.WorkspaceCompleteness{
		FilesDiscoveredTotal:      len(discovered),
		FilesIndexed:              len(indexed),
		IndexTruncated:            previous.IndexTruncated,
		Incomplete:                previous.IndexTruncated || !completeCoverage || previous.ResourcesWithoutSemantics > 0,
		ResourcesWithSemantics:    previous.ResourcesWithSemantics,
		ResourcesWithoutSemantics: previous.ResourcesWithoutSemantics,
	})
}

func (m *Manager) updateSemanticFile(repo, resource, filePath, language string, content []byte, symbols []parser.Symbol) error {
	repoID, err := m.store.GetRepoID(repo)
	if err != nil {
		return err
	}
	if filePath == "fxmanifest.lua" || filePath == "__resource.lua" {
		return m.rebuildSemanticRepository(repoID, repo, resource)
	}
	entities, err := m.store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil {
		return err
	}
	manifestFound := false
	side := "unknown"
	for _, entity := range entities {
		if entity.Kind == fivem.KindManifestResource {
			manifestFound = true
			side = fivem.ClassifyPathFromEntity(entity, filePath)
			break
		}
	}
	if !manifestFound {
		return m.store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{})
	}
	result, err := fivem.NewAnalyzer().AnalyzeFile(context.Background(), semantic.FileInput{
		Repo: repo, Resource: resource, File: filePath, Language: language,
		Content: content, Symbols: symbols, Side: side,
	})
	if err != nil {
		clearErr := m.store.ReplaceSemanticFileForAnalyzer(repoID, semantic.AnalyzerFiveM, filePath, nil)
		refreshErr := m.refreshSemanticRelationships(repo)
		if clearErr != nil {
			return fmt.Errorf("fivem analysis failed: %v; clearing stale facts failed: %w", err, clearErr)
		}
		if refreshErr != nil {
			return fmt.Errorf("fivem analysis failed: %v; refreshing relationships failed: %w", err, refreshErr)
		}
		return err
	}
	if err := m.store.ReplaceSemanticFileForAnalyzer(repoID, semantic.AnalyzerFiveM, filePath, result.Entities); err != nil {
		return err
	}
	return m.refreshSemanticRelationships(repo)
}

func (m *Manager) refreshSemanticRelationships(repo string) error {
	repoID, err := m.store.GetRepoID(repo)
	if err != nil {
		return err
	}
	entities, err := m.store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil {
		return err
	}
	return m.store.ReplaceSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerFiveM, fivem.ResolveRelationships(entities))
}

func (m *Manager) updateGenericFile(repo, filePath, language string, content []byte, symbols []parser.Symbol) error {
	repoID, err := m.store.GetRepoID(repo)
	if err != nil {
		return err
	}
	result, err := generic.NewAnalyzer().AnalyzeFile(context.Background(), semantic.FileInput{
		Repo: repo, File: filePath, Language: language, Content: content, Symbols: symbols,
	})
	if err != nil {
		clearErr := m.store.ReplaceSemanticFileForAnalyzer(repoID, semantic.AnalyzerGenericGraph, filePath, nil)
		refreshErr := m.refreshGenericRelationships(repo)
		if clearErr != nil {
			return fmt.Errorf("generic analysis failed: %v; clearing stale facts failed: %w", err, clearErr)
		}
		if refreshErr != nil {
			return fmt.Errorf("generic analysis failed: %v; refreshing relationships failed: %w", err, refreshErr)
		}
		return err
	}
	if err := m.store.ReplaceSemanticFileForAnalyzer(repoID, semantic.AnalyzerGenericGraph, filePath, result.Entities); err != nil {
		return err
	}
	return m.refreshGenericRelationships(repo)
}

func (m *Manager) refreshGenericRelationships(repo string) error {
	repoID, err := m.store.GetRepoID(repo)
	if err != nil {
		return err
	}
	entities, err := m.store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerGenericGraph)
	if err != nil {
		return err
	}
	return m.store.ReplaceSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerGenericGraph, generic.ResolveRelationships(entities))
}

func (m *Manager) rebuildSemanticRepository(repoID int64, repo, resource string) error {
	files, err := m.store.GetFiles(repoID)
	if err != nil {
		return err
	}
	input := semantic.RepositoryInput{
		Repo: repo, Resource: resource, SourceType: "local",
		Files: make(map[string][]byte), Languages: make(map[string]string), Symbols: make(map[string][]parser.Symbol),
	}
	for _, file := range files {
		content, err := m.store.GetFileContent(repoID, file.Path)
		if err != nil {
			continue
		}
		input.Files[file.Path] = content
		input.Languages[file.Path] = file.Language
		input.Symbols[file.Path], _ = m.store.GetSymbolsByFile(repoID, file.Path)
	}
	result, err := fivem.NewAnalyzer().AnalyzeRepository(context.Background(), input)
	if err != nil {
		if clearErr := m.store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{}); clearErr != nil {
			return fmt.Errorf("fivem repository analysis failed: %v; clearing stale facts failed: %w", err, clearErr)
		}
		return err
	}
	return m.store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, result)
}
