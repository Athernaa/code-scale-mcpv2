package storage

import (
	"database/sql"
	"time"
)

var ErrWorkspaceNotFound = sql.ErrNoRows

// WorkspaceInfo is the persisted organization mode for a local repository.
type WorkspaceInfo struct {
	ID                        int64  `json:"id"`
	RepoID                    int64  `json:"repo_id"`
	RootPath                  string `json:"root_path"`
	Kind                      string `json:"kind"`
	IndexedAt                 string `json:"indexed_at"`
	FilesDiscoveredTotal      int    `json:"files_discovered_total"`
	FilesIndexed              int    `json:"files_indexed"`
	IndexTruncated            bool   `json:"index_truncated"`
	Incomplete                bool   `json:"incomplete"`
	ResourcesWithSemantics    int    `json:"resources_with_semantics"`
	ResourcesWithoutSemantics int    `json:"resources_without_semantics"`
}

type WorkspaceResourceInfo struct {
	ID           int64  `json:"id"`
	WorkspaceID  int64  `json:"workspace_id"`
	Name         string `json:"name"`
	RelativePath string `json:"relative_path"`
	ManifestPath string `json:"manifest_path"`
	ManifestType string `json:"manifest_type"`
	EnabledState string `json:"enabled_state"`
	StartOrder   int    `json:"start_order"`
	GroupPath    string `json:"group_path,omitempty"`
}

type WorkspaceConfigInfo struct {
	ID          int64  `json:"id"`
	WorkspaceID int64  `json:"workspace_id"`
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
}

// ReplaceWorkspaceState replaces only the workspace metadata owned by a repo.
func (s *IndexStore) ReplaceWorkspaceState(repoID int64, rootPath, kind string, resources []WorkspaceResourceInfo, configs []WorkspaceConfigInfo, completeness ...WorkspaceCompleteness) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec("DELETE FROM workspaces WHERE repo_id = ?", repoID); err != nil {
		return err
	}
	c := WorkspaceCompleteness{}
	if len(completeness) > 0 {
		c = completeness[0]
	}
	res, err := tx.Exec("INSERT INTO workspaces(repo_id, root_path, kind, indexed_at, files_discovered_total, files_indexed, index_truncated, incomplete, resources_with_semantics, resources_without_semantics) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", repoID, rootPath, kind, time.Now().UTC().Format(time.RFC3339), c.FilesDiscoveredTotal, c.FilesIndexed, boolInt(c.IndexTruncated), boolInt(c.Incomplete), c.ResourcesWithSemantics, c.ResourcesWithoutSemantics)
	if err != nil {
		return err
	}
	wid, err := res.LastInsertId()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO workspace_resources(workspace_id,name,relative_path,manifest_path,manifest_type,enabled_state,start_order,group_path) VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		if _, err = stmt.Exec(wid, resource.Name, resource.RelativePath, resource.ManifestPath, resource.ManifestType, resource.EnabledState, resource.StartOrder, resource.GroupPath); err != nil {
			_ = stmt.Close()
			return err
		}
	}
	_ = stmt.Close()
	stmt, err = tx.Prepare(`INSERT INTO workspace_configs(workspace_id,path,content_hash) VALUES (?,?,?)`)
	if err != nil {
		return err
	}
	for _, config := range configs {
		if _, err = stmt.Exec(wid, config.Path, config.ContentHash); err != nil {
			_ = stmt.Close()
			return err
		}
	}
	_ = stmt.Close()
	return tx.Commit()
}

type WorkspaceCompleteness struct {
	FilesDiscoveredTotal      int
	FilesIndexed              int
	IndexTruncated            bool
	Incomplete                bool
	ResourcesWithSemantics    int
	ResourcesWithoutSemantics int
}

func (s *IndexStore) ClearWorkspaceState(repoID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM workspaces WHERE repo_id = ?", repoID)
	return err
}

// UpdateWorkspaceCompleteness changes only coverage metadata after indexing
// diagnostics are known; it does not rebuild resource or config rows.
func (s *IndexStore) UpdateWorkspaceCompleteness(repoID int64, c WorkspaceCompleteness) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE workspaces SET files_discovered_total=?, files_indexed=?, index_truncated=?, incomplete=?, resources_with_semantics=?, resources_without_semantics=?, indexed_at=? WHERE repo_id=?`, c.FilesDiscoveredTotal, c.FilesIndexed, boolInt(c.IndexTruncated), boolInt(c.Incomplete), c.ResourcesWithSemantics, c.ResourcesWithoutSemantics, time.Now().UTC().Format(time.RFC3339), repoID)
	return err
}

func (s *IndexStore) GetWorkspace(repoID int64) (WorkspaceInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var w WorkspaceInfo
	var truncated, incomplete int
	err := s.db.QueryRow("SELECT id,repo_id,root_path,kind,indexed_at,files_discovered_total,files_indexed,index_truncated,incomplete,resources_with_semantics,resources_without_semantics FROM workspaces WHERE repo_id = ?", repoID).Scan(&w.ID, &w.RepoID, &w.RootPath, &w.Kind, &w.IndexedAt, &w.FilesDiscoveredTotal, &w.FilesIndexed, &truncated, &incomplete, &w.ResourcesWithSemantics, &w.ResourcesWithoutSemantics)
	w.IndexTruncated = truncated != 0
	w.Incomplete = incomplete != 0
	return w, err
}

func (s *IndexStore) GetWorkspaceResources(repoID int64) ([]WorkspaceResourceInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`SELECT r.id,r.workspace_id,r.name,r.relative_path,r.manifest_path,r.manifest_type,r.enabled_state,r.start_order,r.group_path FROM workspace_resources r JOIN workspaces w ON w.id=r.workspace_id WHERE w.repo_id=? ORDER BY r.relative_path`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []WorkspaceResourceInfo
	for rows.Next() {
		var r WorkspaceResourceInfo
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.Name, &r.RelativePath, &r.ManifestPath, &r.ManifestType, &r.EnabledState, &r.StartOrder, &r.GroupPath); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *IndexStore) GetWorkspaceConfigs(repoID int64) ([]WorkspaceConfigInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`SELECT c.id,c.workspace_id,c.path,c.content_hash FROM workspace_configs c JOIN workspaces w ON w.id=c.workspace_id WHERE w.repo_id=? ORDER BY c.path`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []WorkspaceConfigInfo
	for rows.Next() {
		var c WorkspaceConfigInfo
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Path, &c.ContentHash); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func IsNotFound(err error) bool { return err == sql.ErrNoRows }
