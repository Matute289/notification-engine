// Package handlers is the HTTP inbound adapter. Handlers translate HTTP into
// service input and back; they never perform business logic themselves.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
)

// Handler holds references to every service the HTTP surface exposes.
// Fields use the Svc suffix to avoid clashing with the identically-named handler methods.
type Handler struct {
	SubmitSvc          *service.SubmitNotification
	GetSvc             *service.GetNotification
	CreateTemplateSvc  *service.CreateTemplate
	GetTemplateSvc     *service.GetTemplate
	UpdateTemplateSvc  *service.UpdateTemplate
	DeleteTemplateSvc  *service.DeleteTemplate
	ListTemplatesSvc   *service.ListTemplates
	UpdateSettingSvc   *service.UpdateSetting
	RegisterDeviceSvc  *service.RegisterDevice
	DeleteDeviceSvc    *service.DeleteDevice
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func templateToView(t domain.Template) dto.TemplateView {
	return dto.TemplateView{
		ID:          t.ID,
		Name:        t.Name,
		Channel:     string(t.Channel),
		Locale:      t.Locale,
		Subject:     t.Subject,
		Body:        t.Body,
		MediaURLs:   t.MediaURLs,
		Version:     t.Version,
		OwnerUserID: t.OwnerUserID,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}
