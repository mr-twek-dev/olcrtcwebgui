package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ServerInstance is one independently managed OLC RTC process. The compiled
// binary is shared, while every instance has its own settings, YAML and systemd
// unit (olcrtc@<id>.service).
type ServerInstance struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Settings Settings `json:"settings"`
}

type instanceView struct {
	ServerInstance
	Active     bool   `json:"active"`
	Status     string `json:"status"`
	Unit       string `json:"unit"`
	ConfigPath string `json:"configPath"`
}

func validInstanceID(id string) bool {
	if len(id) < 1 || len(id) > 48 || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for _, c := range id {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func (a *App) instancesFile() string { return filepath.Join(a.dataDir, "instances.json") }

func (a *App) instanceConfigDir() string {
	return filepath.Join(filepath.Dir(a.configFile()), "instances")
}

func (a *App) instanceConfigFile(id string) string {
	return filepath.Join(a.instanceConfigDir(), id+".yaml")
}

func (a *App) templateServiceName() (string, error) {
	name, err := normalizedServiceName(a.service)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(name, ".service") + "@.service", nil
}

func (a *App) instanceServiceName(id string) (string, error) {
	if !validInstanceID(id) {
		return "", errors.New("недопустимый идентификатор экземпляра")
	}
	name, err := normalizedServiceName(a.service)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(name, ".service") + "@" + id + ".service", nil
}

func (a *App) loadInstances() ([]ServerInstance, error) {
	a.instancesMu.Lock()
	defer a.instancesMu.Unlock()
	return a.loadInstancesLocked()
}

func (a *App) loadInstancesLocked() ([]ServerInstance, error) {
	b, err := os.ReadFile(a.instancesFile())
	if err == nil {
		var instances []ServerInstance
		if err := jsonUnmarshalStrictEnough(b, &instances); err != nil {
			return nil, fmt.Errorf("прочитать реестр экземпляров: %w", err)
		}
		for i := range instances {
			if !validInstanceID(instances[i].ID) {
				return nil, fmt.Errorf("реестр содержит недопустимый id %q", instances[i].ID)
			}
			instances[i].Settings.Profiles = nil
		}
		return instances, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// One-time migration: old "profiles" become independent processes instead
	// of failover entries inside a single process. The legacy settings file is
	// kept untouched as a recovery copy.
	legacy, loadErr := a.loadSettings()
	if loadErr != nil {
		return nil, loadErr
	}
	instances := make([]ServerInstance, 0, max(1, len(legacy.Profiles)))
	if len(legacy.Profiles) == 0 {
		if legacy.RoomID == "" && legacy.CryptoKey == "" && legacy.CryptoKeyFile == "" {
			legacy = readyDefaultSettings()
		}
		legacy.Profiles = nil
		instances = append(instances, ServerInstance{ID: "main", Name: "Основной сервер", Settings: legacy})
	} else {
		for i, profile := range legacy.Profiles {
			settings := legacy.forProfile(profile)
			settings.Profiles = nil
			name := strings.TrimSpace(profile.Name)
			if name == "" {
				name = fmt.Sprintf("Сервер %d", i+1)
			}
			instances = append(instances, ServerInstance{ID: fmt.Sprintf("server-%d", i+1), Name: name, Settings: settings})
		}
	}
	if err := a.writeInstancesLocked(instances); err != nil {
		return nil, err
	}
	return instances, nil
}

func jsonUnmarshalStrictEnough(b []byte, out any) error {
	if len(strings.TrimSpace(string(b))) == 0 {
		return errors.New("пустой JSON")
	}
	return json.Unmarshal(b, out)
}

func (a *App) writeInstancesLocked(instances []ServerInstance) error {
	if err := os.MkdirAll(a.dataDir, 0700); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i := range instances {
		instances[i].ID = strings.TrimSpace(instances[i].ID)
		instances[i].Name = strings.TrimSpace(instances[i].Name)
		instances[i].Settings.Profiles = nil
		if !validInstanceID(instances[i].ID) || seen[instances[i].ID] {
			return fmt.Errorf("недопустимый или повторяющийся id экземпляра %q", instances[i].ID)
		}
		if instances[i].Name == "" || len([]rune(instances[i].Name)) > 64 {
			return errors.New("название экземпляра должно содержать от 1 до 64 символов")
		}
		if err := validateSettings(instances[i].Settings); err != nil {
			return fmt.Errorf("%s: %w", instances[i].Name, err)
		}
		seen[instances[i].ID] = true
	}
	b, err := json.MarshalIndent(instances, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(a.instancesFile(), b, 0600)
}

func (a *App) writeInstanceConfig(instance ServerInstance) error {
	if !validInstanceID(instance.ID) {
		return errors.New("недопустимый идентификатор экземпляра")
	}
	if err := os.MkdirAll(a.instanceConfigDir(), 0755); err != nil {
		return err
	}
	return atomicWrite(a.instanceConfigFile(instance.ID), []byte(settingsYAML(instance.Settings)), 0600)
}

func findInstance(instances []ServerInstance, id string) (int, bool) {
	for i := range instances {
		if instances[i].ID == id {
			return i, true
		}
	}
	return -1, false
}

func nextInstanceID(instances []ServerInstance) string {
	used := map[string]bool{}
	for _, instance := range instances {
		used[instance.ID] = true
	}
	for i := 1; ; i++ {
		id := fmt.Sprintf("server-%d", i)
		if !used[id] {
			return id
		}
	}
}

func validateInstanceConflicts(settings Settings, instances []ServerInstance, excludeID string) error {
	if settings.Mode != "cnc" {
		return nil
	}
	for _, instance := range instances {
		if instance.ID == excludeID || instance.Settings.Mode != "cnc" {
			continue
		}
		if strings.EqualFold(instance.Settings.SOCKS.Host, settings.SOCKS.Host) && instance.Settings.SOCKS.Port == settings.SOCKS.Port {
			return fmt.Errorf("адрес SOCKS5 %s:%d уже используется экземпляром %q", settings.SOCKS.Host, settings.SOCKS.Port, instance.Name)
		}
	}
	return nil
}

func (a *App) instanceState(id string) (bool, string) {
	if !a.installationReady() {
		return false, "не установлен"
	}
	unit, err := a.instanceServiceName(id)
	if err != nil {
		return false, "ошибка"
	}
	if _, err := a.runner("systemctl", "is-active", "--quiet", unit); err == nil {
		return true, "работает"
	}
	if _, err := a.runner("systemctl", "is-failed", "--quiet", unit); err == nil {
		return false, "ошибка"
	}
	return false, "остановлен"
}

func (a *App) listInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := a.loadInstances()
	if err != nil {
		apiError(w, 500, err.Error())
		return
	}
	views := make([]instanceView, 0, len(instances))
	for _, instance := range instances {
		active, status := a.instanceState(instance.ID)
		unit, _ := a.instanceServiceName(instance.ID)
		views = append(views, instanceView{ServerInstance: instance, Active: active, Status: status, Unit: unit, ConfigPath: a.instanceConfigFile(instance.ID)})
	}
	jsonOut(w, 200, map[string]any{"instances": views})
}

func (a *App) createInstance(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string    `json:"name"`
		Settings *Settings `json:"settings"`
	}
	if !decode(w, r, &input) {
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 64 {
		apiError(w, 400, "название экземпляра должно содержать от 1 до 64 символов")
		return
	}
	settings := readyDefaultSettings()
	if input.Settings != nil {
		settings = *input.Settings
	}
	settings.Profiles = nil
	if err := validateSettings(settings); err != nil {
		apiError(w, 400, err.Error())
		return
	}

	a.instancesMu.Lock()
	defer a.instancesMu.Unlock()
	instances, err := a.loadInstancesLocked()
	if err != nil {
		apiError(w, 500, err.Error())
		return
	}
	if err := validateInstanceConflicts(settings, instances, ""); err != nil {
		apiError(w, 409, err.Error())
		return
	}
	instance := ServerInstance{ID: nextInstanceID(instances), Name: name, Settings: settings}
	if err := a.writeInstanceConfig(instance); err != nil {
		apiError(w, 500, err.Error())
		return
	}
	instances = append(instances, instance)
	if err := a.writeInstancesLocked(instances); err != nil {
		_ = os.Remove(a.instanceConfigFile(instance.ID))
		apiError(w, 500, err.Error())
		return
	}
	if a.installationReady() {
		unit, _ := a.instanceServiceName(instance.ID)
		if out, err := a.runner("systemctl", "enable", unit); err != nil {
			_ = a.writeInstancesLocked(instances[:len(instances)-1])
			_ = os.Remove(a.instanceConfigFile(instance.ID))
			apiError(w, 500, "не удалось включить автозапуск экземпляра: "+commandError(out, err))
			return
		}
	}
	active, status := a.instanceState(instance.ID)
	unit, _ := a.instanceServiceName(instance.ID)
	jsonOut(w, http.StatusCreated, instanceView{ServerInstance: instance, Active: active, Status: status, Unit: unit, ConfigPath: a.instanceConfigFile(instance.ID)})
}

func (a *App) updateInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validInstanceID(id) {
		apiError(w, 400, "недопустимый идентификатор экземпляра")
		return
	}
	var input struct {
		Name     string   `json:"name"`
		Settings Settings `json:"settings"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Settings.Profiles = nil
	if input.Name == "" || len([]rune(input.Name)) > 64 {
		apiError(w, 400, "название экземпляра должно содержать от 1 до 64 символов")
		return
	}
	if err := validateSettings(input.Settings); err != nil {
		apiError(w, 400, err.Error())
		return
	}

	a.instancesMu.Lock()
	defer a.instancesMu.Unlock()
	instances, err := a.loadInstancesLocked()
	if err != nil {
		apiError(w, 500, err.Error())
		return
	}
	index, ok := findInstance(instances, id)
	if !ok {
		apiError(w, 404, "экземпляр не найден")
		return
	}
	if err := validateInstanceConflicts(input.Settings, instances, id); err != nil {
		apiError(w, 409, err.Error())
		return
	}
	updated := ServerInstance{ID: id, Name: input.Name, Settings: input.Settings}
	if err := a.writeInstanceConfig(updated); err != nil {
		apiError(w, 500, err.Error())
		return
	}
	instances[index] = updated
	if err := a.writeInstancesLocked(instances); err != nil {
		apiError(w, 500, err.Error())
		return
	}
	active, status := a.instanceState(id)
	unit, _ := a.instanceServiceName(id)
	jsonOut(w, 200, instanceView{ServerInstance: updated, Active: active, Status: status, Unit: unit, ConfigPath: a.instanceConfigFile(id)})
}

func (a *App) deleteInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validInstanceID(id) {
		apiError(w, 400, "недопустимый идентификатор экземпляра")
		return
	}
	a.instancesMu.Lock()
	defer a.instancesMu.Unlock()
	instances, err := a.loadInstancesLocked()
	if err != nil {
		apiError(w, 500, err.Error())
		return
	}
	index, ok := findInstance(instances, id)
	if !ok {
		apiError(w, 404, "экземпляр не найден")
		return
	}
	if a.installationReady() {
		unit, _ := a.instanceServiceName(id)
		if out, err := a.runner("systemctl", "disable", "--now", unit); err != nil {
			apiError(w, 500, commandError(out, err))
			return
		}
	}
	instances = append(instances[:index], instances[index+1:]...)
	if err := a.writeInstancesLocked(instances); err != nil {
		apiError(w, 500, err.Error())
		return
	}
	if err := os.Remove(a.instanceConfigFile(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		apiError(w, 500, "реестр обновлён, но удалить YAML не удалось: "+err.Error())
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (a *App) instanceServiceAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	if !validInstanceID(id) || (action != "start" && action != "stop" && action != "restart") {
		apiError(w, 400, "недопустимый экземпляр или действие")
		return
	}
	instances, err := a.loadInstances()
	if err != nil {
		apiError(w, 500, err.Error())
		return
	}
	index, ok := findInstance(instances, id)
	if !ok {
		apiError(w, 404, "экземпляр не найден")
		return
	}
	if !a.installationReady() {
		apiError(w, http.StatusConflict, "OLC RTC не установлен. Нажмите «Установить / обновить»")
		return
	}
	if action != "stop" {
		if err := a.writeInstanceConfig(instances[index]); err != nil {
			apiError(w, 500, err.Error())
			return
		}
	}
	unit, _ := a.instanceServiceName(id)
	out, err := a.runner("systemctl", action, unit)
	if err != nil {
		apiError(w, 500, commandError(out, err))
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (a *App) shareInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validInstanceID(id) {
		apiError(w, 400, "недопустимый идентификатор экземпляра")
		return
	}
	var input struct {
		Comment string `json:"comment"`
	}
	if !decode(w, r, &input) {
		return
	}
	instances, err := a.loadInstances()
	if err != nil {
		apiError(w, 500, err.Error())
		return
	}
	index, ok := findInstance(instances, id)
	if !ok {
		apiError(w, 404, "экземпляр не найден")
		return
	}
	a.shareSettings(w, instances[index].Settings, input.Comment, a.instanceConfigFile(id))
}

