package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var defaultJitsiResources = []string{
	"https://meet.egovm.ru",
	"https://conference.ct.placetime.team",
	"https://meet.riddlerx.org",
}

type jitsiResourcesResponse struct {
	Resources []string `json:"resources"`
}

func (a *App) jitsiResourcesFile() string {
	return filepath.Join(a.dataDir, "jitsi-resources.json")
}

func normalizeJitsiResource(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("адрес Jitsi не может быть пустым")
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("некорректный адрес Jitsi %q", value)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", errors.New("адрес Jitsi должен использовать http или https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("адрес Jitsi не должен содержать логин, параметры или фрагмент")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeJitsiResources(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("добавьте хотя бы один Jitsi-ресурс")
	}
	if len(values) > 32 {
		return nil, errors.New("можно сохранить не более 32 Jitsi-ресурсов")
	}
	seen := make(map[string]bool, len(values))
	resources := make([]string, 0, len(values))
	for _, value := range values {
		resource, err := normalizeJitsiResource(value)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(resource)
		if !seen[key] {
			seen[key] = true
			resources = append(resources, resource)
		}
	}
	return resources, nil
}

func (a *App) loadJitsiResourcesLocked() ([]string, error) {
	b, err := os.ReadFile(a.jitsiResourcesFile())
	if errors.Is(err, os.ErrNotExist) {
		return append([]string(nil), defaultJitsiResources...), nil
	}
	if err != nil {
		return nil, err
	}
	var stored jitsiResourcesResponse
	if err := jsonUnmarshalStrictEnough(b, &stored); err != nil {
		return nil, fmt.Errorf("прочитать Jitsi-ресурсы: %w", err)
	}
	return normalizeJitsiResources(stored.Resources)
}

func (a *App) loadJitsiResources() ([]string, error) {
	a.jitsiMu.Lock()
	defer a.jitsiMu.Unlock()
	return a.loadJitsiResourcesLocked()
}

func (a *App) getJitsiResources(w http.ResponseWriter, _ *http.Request) {
	resources, err := a.loadJitsiResources()
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOut(w, http.StatusOK, jitsiResourcesResponse{Resources: resources})
}

func (a *App) saveJitsiResources(w http.ResponseWriter, r *http.Request) {
	var input jitsiResourcesResponse
	if !decode(w, r, &input) {
		return
	}
	resources, err := normalizeJitsiResources(input.Resources)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.jitsiMu.Lock()
	defer a.jitsiMu.Unlock()
	if err := os.MkdirAll(a.dataDir, 0700); err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	b, err := json.MarshalIndent(jitsiResourcesResponse{Resources: resources}, "", "  ")
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := atomicWrite(a.jitsiResourcesFile(), b, 0600); err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOut(w, http.StatusOK, jitsiResourcesResponse{Resources: resources})
}

func newJitsiRoomName() string {
	return "olcrtc-" + strings.ToLower(random(9))
}

func (a *App) createJitsiRoom(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Resource string `json:"resource"`
	}
	if !decode(w, r, &input) {
		return
	}
	resource, err := normalizeJitsiResource(input.Resource)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	resources, err := a.loadJitsiResources()
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	allowed := false
	for _, candidate := range resources {
		if strings.EqualFold(candidate, resource) {
			allowed = true
			resource = candidate
			break
		}
	}
	if !allowed {
		apiError(w, http.StatusBadRequest, "выберите Jitsi-ресурс из сохранённого списка")
		return
	}
	jsonOut(w, http.StatusCreated, map[string]string{"roomUrl": resource + "/" + newJitsiRoomName()})
}

