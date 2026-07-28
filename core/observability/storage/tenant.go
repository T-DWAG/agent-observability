package storage

// normalizeTenantID 空串视为 default，读写两侧统一。
func normalizeTenantID(id string) string {
	if id == "" {
		return "default"
	}
	return id
}
