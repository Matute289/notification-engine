package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serviceHandler wires a CreateTemplate service and returns a Handler ready to
// receive requests authenticated as a service identity on behalf of userID 42.
func buildCreateTemplateHandler(repo *templateRepo) *Handler {
	return &Handler{CreateTemplateSvc: &service.CreateTemplate{
		Templates: repo,
		Clock:     fixedClock{},
	}}
}

func TestCreateTemplate_HappyPath_201(t *testing.T) {
	h := buildCreateTemplateHandler(&templateRepo{})
	w := httptest.NewRecorder()
	body := `{"name":"welcome","channel":"sms","body":"Hello {{.Name}}!","version":1}`
	r := withServiceIdentity(
		httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(body)),
		42,
	)
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusCreated, w.Code)
	var t2 dto.TemplateView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&t2))
	assert.Equal(t, "welcome", t2.Name)
	assert.Equal(t, "sms", t2.Channel)
	assert.Equal(t, int64(42), t2.OwnerUserID)
}

func TestCreateTemplate_NoIdentity_403(t *testing.T) {
	h := &Handler{CreateTemplateSvc: &service.CreateTemplate{}}
	w := httptest.NewRecorder()
	body := `{"name":"welcome","channel":"sms","body":"Hello!","version":1}`
	r := httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(body))
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "forbidden")
}

func TestCreateTemplate_ServiceNoOnBehalfOf_403(t *testing.T) {
	h := &Handler{CreateTemplateSvc: &service.CreateTemplate{}}
	w := httptest.NewRecorder()
	body := `{"name":"welcome","channel":"sms","body":"Hello!","version":1}`
	// onBehalfOfUserID=0 means no OnBehalfOfUserID set
	r := withServiceIdentity(
		httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(body)),
		0,
	)
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "forbidden")
}

func TestCreateTemplate_InvalidJSON_400(t *testing.T) {
	h := &Handler{CreateTemplateSvc: &service.CreateTemplate{}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(
		httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(`{bad}`)),
		42,
	)
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_json")
}

func TestCreateTemplate_InvalidChannel_400(t *testing.T) {
	h := &Handler{CreateTemplateSvc: &service.CreateTemplate{}}
	w := httptest.NewRecorder()
	body := `{"name":"welcome","channel":"fax","body":"Hello!","version":1}`
	r := withServiceIdentity(
		httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(body)),
		42,
	)
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_channel")
}

func TestCreateTemplate_AlreadyExists_409(t *testing.T) {
	h := buildCreateTemplateHandler(&templateRepo{err: domain.ErrAlreadyExists})
	w := httptest.NewRecorder()
	body := `{"name":"welcome","channel":"sms","body":"Hello!","version":1}`
	r := withServiceIdentity(
		httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(body)),
		42,
	)
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusConflict, w.Code)
	assertErrorCode(t, w, "conflict")
}

func TestCreateTemplate_MissingBody_400(t *testing.T) {
	h := buildCreateTemplateHandler(&templateRepo{})
	w := httptest.NewRecorder()
	// body field is empty → domain.NewTemplate returns ErrInvalidInput
	body := `{"name":"welcome","channel":"sms","body":"","version":1}`
	r := withServiceIdentity(
		httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(body)),
		42,
	)
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_request")
}
