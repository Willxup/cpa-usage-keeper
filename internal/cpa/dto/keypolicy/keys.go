package keypolicy

type Key struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	KeyPreview string `json:"key_preview"`
	Enabled    bool   `json:"enabled"`
}

type KeysResponse struct {
	Keys []Key `json:"keys"`
}
