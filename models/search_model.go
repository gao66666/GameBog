package models

type SearchSyncPayload struct {
	ID         uint64   `json:"id"`
	Title      string   `json:"title"`
	AuthorName string   `json:"author_name"`
	Tags       []string `json:"tags"`
	Summary    string   `json:"summary"`
}
