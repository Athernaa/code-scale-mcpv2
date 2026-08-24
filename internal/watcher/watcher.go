package watcher

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
	"github.com/Athernaa/code-scale-mcpv2/internal/security"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/summarizer"
)

const debounceInterval = 500 * time.Millisecond

// FolderWatch tracks a single watched folder.
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
					}
					continue
				}
			}

			// Only track supported source files
			lang := parser.DetectLanguage(event.Name)
			if lang == "" {
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

// reindexFiles re-parses and updates the index for changed files.
func (m *Manager) reindexFiles(fw *FolderWatch, paths []string) {
	localID, err := repository.Local(fw.Path)
	if err != nil {
		log.Printf("watcher: cannot resolve repository identity for %s: %v", fw.Path, err)
		return
	}
	owner := localID.Owner
	repoName := localID.Name

	for _, fullPath := range paths {
		relPath, err := filepath.Rel(fw.Path, fullPath)
		if err != nil {
			continue
		}
		relPath = filepath.ToSlash(relPath)

		// Check if file was deleted
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			log.Printf("watcher: file removed %s, cleaning index", relPath)
			if err := m.store.DeleteFileFromIndex(owner, repoName, relPath); err != nil {
				log.Printf("watcher: failed to remove %s from index: %v", relPath, err)
			}
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

		// Save content file
		if err := m.store.SaveContentFile(owner, repoName, relPath, content); err != nil {
			log.Printf("watcher: failed to save content for %s: %v", relPath, err)
		}

		// Save only this file's index; a full replacement would erase unrelated files.
		err = m.store.UpsertFileIndex(owner, repoName, "local", "", relPath, hash, lang, symbols, localID.CanonicalPath)
		if err != nil {
			log.Printf("watcher: save error for %s: %v", relPath, err)
			continue
		}

		log.Printf("watcher: reindexed %s (%d symbols)", relPath, len(symbols))
	}
}
