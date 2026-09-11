package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyProfilesMigrateToIndependentInstances(t *testing.T) {
	a := testApp(t)
	legacy := validSettings()
	legacy.Profiles = []ProfileSettings{
		{Name: "Jitsi", ConnectionSettings: legacy.ConnectionSettings},
		{Name: "WB", ConnectionSettings: legacy.ConnectionSettings},
	}
	legacy.Profiles[1].Provider = "wbstream"
	legacy.Profiles[1].ProviderToken = "token"
	if err := a.writeSettings(legacy); err != nil {
		t.Fatal(err)
	}
	instances, err := a.loadInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 || instances[0].ID != "server-1" || instances[1].Name != "WB" {
		t.Fatalf("unexpected migration: %#v", instances)
	}
	if len(instances[0].Settings.Profiles) != 0 || instances[1].Settings.Provider != "wbstream" {
		t.Fatalf("profiles were not flattened: %#v", instances)
	}
	if _, err := os.Stat(filepath.Join(a.dataDir, "settings.json")); err != nil {
		t.Fatalf("legacy recovery copy removed: %v", err)
	}
}

func TestInstanceCRUDAndSeparateConfigs(t *testing.T) {
	a := testApp(t)
	cookie := loginCookie(t, a)
	settings := validSettings()
	body, _ := json.Marshal(map[string]any{"name": "Jitsi Frankfurt", "settings": settings})
	created := request(a, http.MethodPost, "/api/instances", string(body), cookie)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var instance instanceView
	if err := json.Unmarshal(created.Body.Bytes(), &instance); err != nil {
		t.Fatal(err)
	}
	if instance.ID != "server-1" || instance.Unit != "olcrtc@server-1.service" {
		t.Fatalf("unexpected instance: %#v", instance)
	}
	if _, err := os.Stat(a.instanceConfigFile(instance.ID)); err != nil {
		t.Fatalf("instance YAML missing: %v", err)
	}

	settings.RoomID = "https://meet.example.org/second-room"
	updateBody, _ := json.Marshal(map[string]any{"name": "Jitsi Paris", "settings": settings})
	updated := request(a, http.MethodPut, "/api/instances/"+instance.ID, string(updateBody), cookie)
	if updated.Code != http.StatusOK {
		t.Fatalf("update: %d %s", updated.Code, updated.Body.String())
	}
	yaml, err := os.ReadFile(a.instanceConfigFile(instance.ID))
	if err != nil || !strings.Contains(string(yaml), "second-room") {
		t.Fatalf("separate YAML not updated: %v %s", err, yaml)
	}
	shared := request(a, http.MethodPost, "/api/instances/"+instance.ID+"/share", `{"comment":"Paris"}`, cookie)
	if shared.Code != http.StatusOK || !strings.Contains(shared.Body.String(), "second-room") {
		t.Fatalf("share current instance: %d %s", shared.Code, shared.Body.String())
	}
	listed := request(a, http.MethodGet, "/api/instances", "", cookie)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "Jitsi Paris") {
		t.Fatalf("list: %d %s", listed.Code, listed.Body.String())
	}
}

func TestInstanceActionTargetsOnlyRequestedUnit(t *testing.T) {
	a := testApp(t)
	if err := os.MkdirAll(filepath.Dir(a.binaryFile()), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.binaryFile(), []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(a.systemdDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.serviceFile(), []byte("unit"), 0644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	a.runner = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		return nil, nil
	}
	w := request(a, http.MethodPost, "/api/instances/main/service/restart", "", loginCookie(t, a))
	if w.Code != http.StatusOK {
		t.Fatalf("restart: %d %s", w.Code, w.Body.String())
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "systemctl restart olcrtc@main.service") || strings.Contains(joined, "olcrtc.service") {
		t.Fatalf("wrong unit targeted:\n%s", joined)
	}
}

func TestInstanceIDsRejectPathTraversal(t *testing.T) {
	for _, id := range []string{"../main", "Main", "main_service", "main@evil", ""} {
		if validInstanceID(id) {
			t.Errorf("unsafe id accepted: %q", id)
		}
	}
	for _, id := range []string{"main", "server-2", "a1"} {
		if !validInstanceID(id) {
			t.Errorf("safe id rejected: %q", id)
		}
	}
}

func TestCNCInstancesCannotBindSameSOCKSPort(t *testing.T) {
	first := validSettings()
	first.Mode = "cnc"
	first.SOCKS.Host = "127.0.0.1"
	first.SOCKS.Port = 8808
	second := first
	instances := []ServerInstance{{ID: "main", Name: "First", Settings: first}}
	if err := validateInstanceConflicts(second, instances, ""); err == nil {
		t.Fatal("duplicate SOCKS listener was accepted")
	}
	second.SOCKS.Port = 8809
	if err := validateInstanceConflicts(second, instances, ""); err != nil {
		t.Fatalf("different SOCKS listener rejected: %v", err)
	}
}

