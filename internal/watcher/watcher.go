package watcher

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/syphon1c/code-scale-mcp/internal/parser"
	"github.com/syphon1c/code-scale-mcp/internal/security"
	"github.com/syphon1c/code-scale-mcp/internal/storage"
	"github.com/syphon1c/code-scale-mcp/internal/summarizer"
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

	if _, exists := m.watches[absPath]; exists {
		return nil // Already watching
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Add all subdirectories recursively
	err = filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if security.ShouldSkipDir(d.Name()) && path != absPath {
				return filepath.SkipDir
			}
			return w.Add(path)
		}
		return nil
	})
	if err != nil {
		_ = w.Close()
		return err
	}

	folderName := filepath.Base(absPath)
	repo := "local/" + folderName

	fw := &FolderWatch{
		Path:      absPath,
		Repo:      repo,
		StartedAt: time.Now(),
		watcher:   w,
		stop:      make(chan struct{}),
	}
	m.watches[absPath] = fw

	go m.watchLoop(fw)

	return nil
}

// Unwatch stops watching a folder.
func (m *Manager) Unwatch(absPath string) error {
	m.mu.Lock()
	fw, exists := m.watches[absPath]
	if !exists {
		m.mu.Unlock()
		return nil
	}
	delete(m.watches, absPath)
	m.mu.Unlock()

	close(fw.stop)
	return fw.watcher.Close()
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

			// If a new directory was created, watch it
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if !security.ShouldSkipDir(filepath.Base(event.Name)) {
						_ = fw.watcher.Add(event.Name)
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
	owner := "local"
	repoName := filepath.Base(fw.Path)

	for _, fullPath := range paths {
		relPath, err := filepath.Rel(fw.Path, fullPath)
		if err != nil {
			continue
		}
		relPath = filepath.ToSlash(relPath)

		// Check if file was deleted
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			// File deleted — we could remove its symbols here
			// For now, the next full reindex will clean it up
			log.Printf("watcher: file removed %s", relPath)
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

		fileHashes := map[string]string{relPath: hash}
		fileLangs := map[string]string{relPath: lang}

		// Save content file
		_ = m.store.SaveContentFile(owner, repoName, relPath, content)

		// Save index (incremental)
		err = m.store.SaveIndex(owner, repoName, "local", "", fileHashes, fileLangs, symbols)
		if err != nil {
			log.Printf("watcher: save error for %s: %v", relPath, err)
			continue
		}

		log.Printf("watcher: reindexed %s (%d symbols)", relPath, len(symbols))
	}
}
