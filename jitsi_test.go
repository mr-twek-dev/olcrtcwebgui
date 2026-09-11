package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestJitsiResourcesDefaultsAndPersistence(t *testing.T) {
	a := testApp(t)
	cookie := loginCookie(t, a)
	defaults := request(a, http.MethodGet, "/api/jitsi-resources", "", cookie)
	if defaults.Code != http.StatusOK {
		t.Fatalf("defaults: %d %s", defaults.Code, defaults.Body.String())
	}
	for _, resource := range defaultJitsiResources {
		if !strings.Contains(defaults.Body.String(), resource) {
			t.Fatalf("missing default %q: %s", resource, defaults.Body.String())
		}
	}

	saved := request(a, http.MethodPut, "/api/jitsi-resources", `{"resources":["meet.example.org/","https://MEET.example.org","http://local.test/jitsi"]}`, cookie)
	if saved.Code != http.StatusOK {
		t.Fatalf("save: %d %s", saved.Code, saved.Body.String())
	}
	var response jitsiResourcesResponse
	if err := json.Unmarshal(saved.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Resources) != 2 || response.Resources[0] != "https://meet.example.org" || response.Resources[1] != "http://local.test/jitsi" {
		t.Fatalf("unexpected normalized resources: %#v", response.Resources)
	}
	if _, err := os.Stat(a.jitsiResourcesFile()); err != nil {
		t.Fatalf("resources file not persisted: %v", err)
	}
}

func TestJitsiResourcesRejectUnsafeAddresses(t *testing.T) {
	for _, value := range []string{"ftp://meet.example.org", "https://user:pass@meet.example.org", "https://meet.example.org?room=x", "not a host"} {
		if _, err := normalizeJitsiResource(value); err == nil {
			t.Errorf("unsafe address accepted: %q", value)
		}
	}
}

func TestCreateJitsiRoomUsesSavedResource(t *testing.T) {
	a := testApp(t)
	cookie := loginCookie(t, a)
	request(a, http.MethodPut, "/api/jitsi-resources", `{"resources":["https://meet.example.org"]}`, cookie)
	created := request(a, http.MethodPost, "/api/jitsi-room", `{"resource":"https://meet.example.org"}`, cookie)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"roomUrl":"https://meet.example.org/olcrtc-`) {
		t.Fatalf("create room: %d %s", created.Code, created.Body.String())
	}
	rejected := request(a, http.MethodPost, "/api/jitsi-room", `{"resource":"https://unknown.example.org"}`, cookie)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("unknown resource accepted: %d %s", rejected.Code, rejected.Body.String())
	}
}

func TestReadyDefaultsUseConfiguredJitsiCatalogDefault(t *testing.T) {
	s := readyDefaultSettings()
	if !strings.HasPrefix(s.RoomID, defaultJitsiResources[0]+"/olcrtc-") {
		t.Fatalf("unexpected default room: %s", s.RoomID)
	}
}

