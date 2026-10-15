package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
)

// maskedCustomProvider is the external shape of a page-defined platform. It
// holds no secrets: the API key travels per project like every other provider.
type maskedCustomProvider struct {
	model.CustomProvider
}

// handleListCustomProviders lists the page-defined platforms. Reads stay open
// so the page can show what exists even without dynamic configuration.
func (s *Server) handleListCustomProviders(w http.ResponseWriter, r *http.Request) {
	list := s.Resolver.CustomProviders(r.Context())
	if list == nil {
		list = []model.CustomProvider{}
	}
	etagJSON(w, r, map[string]any{"status": "success", "providers": list})
}

type customProviderRequest struct {
	Name          string   `json:"name"`
	DisplayName   *string  `json:"display_name"`
	URL           *string  `json:"url"`
	AuthMode      *string  `json:"auth_mode"`
	AuthParam     *string  `json:"auth_param"`
	DefaultType   *string  `json:"default_type"`
	Currency      *string  `json:"currency"`
	BalancePath   *string  `json:"balance_path"`
	BalanceScale  *float64 `json:"balance_scale"`
	SuccessPath   *string  `json:"success_path"`
	SuccessValues *string  `json:"success_values"`
	ErrorPath     *string  `json:"error_path"`

	SubtractPath     *string  `json:"subtract_path"`
	ExtractMode      *string  `json:"extract_mode"`
	WindowTypes      *string  `json:"window_types"`
	WindowResetHours *float64 `json:"window_reset_hours"`
	ExtraParams      *string  `json:"extra_params"`
	SignRegion       *string  `json:"sign_region"`
	SignService      *string  `json:"sign_service"`
}

// handleSaveCustomProvider creates or updates a page-defined platform. name is
// the unique key; updates change only the provided fields.
func (s *Server) handleSaveCustomProvider(w http.ResponseWriter, r *http.Request) {
	if !s.requireDynamicConfig(w, "custom providers") {
		return
	}
	var body customProviderRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.ToLower(trimSpace(body.Name))
	if body.Name == "" {
		failValidation(w, []string{"name: cannot be empty"})
		return
	}
	// a name equal to a built-in key would register custom:<name>, which
	// works but reads badly on every screen; reject it outright
	if _, builtin := provider.Lookup(body.Name); builtin {
		fail(w, http.StatusBadRequest, body.Name+" is a built-in provider; pick another name")
		return
	}

	existing := findCustomProvider(s.Resolver.CustomProviders(r.Context()), body.Name)
	target := model.CustomProvider{Name: body.Name, AuthMode: provider.CustomAuthBearer, DefaultType: model.TypeBalance}
	if existing != nil {
		target = *existing
	}
	applyCustomProviderPatch(&target, body)

	if problems := provider.ValidateCustomProvider(target); len(problems) > 0 {
		failValidation(w, problems)
		return
	}

	if err := s.Store.UpsertCustomProvider(r.Context(), target); err != nil {
		fail(w, storeWriteStatus(err), "Save failed")
		return
	}
	s.Resolver.Load(r.Context())
	s.log().Info("[AUDIT] Custom provider saved", "provider", target.Name)
	ok(w, map[string]any{"message": "Custom provider [" + target.Name + "] saved"})
}

func (s *Server) handleDeleteCustomProvider(w http.ResponseWriter, r *http.Request) {
	if !s.requireDynamicConfig(w, "custom providers") {
		return
	}
	var body nameRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.ToLower(trimSpace(body.Name))
	if body.Name == "" {
		failValidation(w, []string{"name: cannot be empty"})
		return
	}

	key := model.CustomProviderKey(body.Name)
	cfg := s.Resolver.Load(r.Context())
	for _, p := range cfg.Projects {
		if p.Provider == key {
			fail(w, http.StatusBadRequest, "Provider ["+key+"] is still used by project "+p.Name+"; delete or rebind it first")
			return
		}
	}

	if err := s.Store.DeleteCustomProvider(r.Context(), body.Name); err != nil {
		fail(w, storeWriteStatus(err), "Delete failed")
		return
	}
	s.Resolver.Load(r.Context())
	s.log().Info("[AUDIT] Custom provider deleted", "provider", body.Name)
	ok(w, map[string]any{"message": "Custom provider [" + body.Name + "] deleted"})
}

// handleTestCustomProvider runs a (possibly unsaved) custom provider once
// against its real endpoint, so the form can prove the wiring before saving.
func (s *Server) handleTestCustomProvider(w http.ResponseWriter, r *http.Request) {
	if !s.requireDynamicConfig(w, "custom providers") {
		return
	}
	var body struct {
		customProviderRequest
		APIKey string `json:"api_key"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name == "" {
		failValidation(w, []string{"name: cannot be empty"})
		return
	}
	target := model.CustomProvider{Name: strings.ToLower(trimSpace(body.Name))}
	applyCustomProviderPatch(&target, body.customProviderRequest)

	if problems := provider.ValidateCustomProvider(target); len(problems) > 0 {
		failValidation(w, problems)
		return
	}
	spec, err := provider.SpecFromCustomProvider(target)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	client := provider.NewClient(time.Duration(s.Settings.RequestTimeout) * time.Second)
	value, err := provider.NewFromSpec(spec, trimSpace(body.APIKey), client).Fetch(r.Context())
	if err != nil {
		fail(w, http.StatusBadRequest, "Fetch failed: "+err.Error())
		return
	}
	ok(w, map[string]any{"message": "Wiring works", "value": value, "type": target.DefaultType})
}

func applyCustomProviderPatch(target *model.CustomProvider, body customProviderRequest) {
	if body.DisplayName != nil {
		target.DisplayName = trimSpace(*body.DisplayName)
	}
	if body.URL != nil {
		target.URL = trimSpace(*body.URL)
	}
	if body.AuthMode != nil {
		target.AuthMode = strings.ToLower(trimSpace(*body.AuthMode))
	}
	if body.AuthParam != nil {
		target.AuthParam = trimSpace(*body.AuthParam)
	}
	if body.DefaultType != nil && *body.DefaultType != "" {
		target.DefaultType = strings.ToLower(trimSpace(*body.DefaultType))
	}
	if body.Currency != nil {
		target.Currency = strings.ToUpper(trimSpace(*body.Currency))
	}
	if target.DefaultType == model.TypeQuota {
		target.Currency = ""
	}
	if body.BalancePath != nil {
		target.BalancePath = trimSpace(*body.BalancePath)
	}
	if body.BalanceScale != nil && *body.BalanceScale > 0 {
		target.BalanceScale = *body.BalanceScale
	}
	if body.SuccessPath != nil {
		target.SuccessPath = trimSpace(*body.SuccessPath)
	}
	if body.SuccessValues != nil {
		target.SuccessValues = trimSpace(*body.SuccessValues)
	}
	if body.ErrorPath != nil {
		target.ErrorPath = trimSpace(*body.ErrorPath)
	}
	if body.SubtractPath != nil {
		target.SubtractPath = trimSpace(*body.SubtractPath)
	}
	if body.ExtractMode != nil {
		target.ExtractMode = strings.ToLower(trimSpace(*body.ExtractMode))
	}
	if body.WindowTypes != nil {
		target.WindowTypes = trimSpace(*body.WindowTypes)
	}
	if body.WindowResetHours != nil && *body.WindowResetHours >= 0 {
		target.WindowResetHours = *body.WindowResetHours
	}
	if body.ExtraParams != nil {
		target.ExtraParams = trimSpace(*body.ExtraParams)
	}
	if body.SignRegion != nil {
		target.SignRegion = trimSpace(*body.SignRegion)
	}
	if body.SignService != nil {
		target.SignService = trimSpace(*body.SignService)
	}
}

func findCustomProvider(list []model.CustomProvider, name string) *model.CustomProvider {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}
