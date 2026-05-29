package dto

type UpdateTemplateRequest struct {
	Name      string   `json:"name"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
	MediaURLs []string `json:"media_urls,omitempty"`
}
