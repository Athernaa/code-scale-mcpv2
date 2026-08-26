package isolationb

// REPO_B_ONLY_MARKER must never cross the repository boundary.
func LoadConfig() string { return "REPO_B_ONLY_MARKER" }
