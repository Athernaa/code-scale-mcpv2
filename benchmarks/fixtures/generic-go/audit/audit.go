package audit

func RecordLogin(userID string) string { return "login:" + userID }

func RecordAdminAction(userID string) string { return "admin:" + userID }
