package dto

type TemplateRequest struct {
	Name      string   `json:"name"`
	Channel   string   `json:"channel"`
	Locale    string   `json:"locale"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
	MediaURLs []string `json:"media_urls,omitempty"`
	Version   int      `json:"version"`
}
