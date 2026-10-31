package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadServiceEndpoints(t *testing.T) {
	tests := []struct {
		name, services, wantError string
	}{
		{"field device", `[{"roles":["field-device"],"endpoints":{"sim":"http://10.99.99.22:8080"}}]`, ""},
		{"time source", `[{"roles":["time-source"],"endpoints":{"sim":"http://10.99.99.27:8080"}}]`, ""},
		{"many services", `[{"roles":["field-device"],"endpoints":{"sim":"http://10.99.99.22:8080"}},{"roles":["time-source"],"endpoints":{"sim":"http://10.99.99.27:8080"}}]`, ""},
		{"two sims on one field device", `[{"roles":["field-device"],"endpoints":{"sim":"http://10.99.99.22:8080","sim":"http://10.99.99.22:8081"}}]`, `duplicate JSON key "sim" at $.services[0].endpoints`},
		{"two sims on one time source", `[{"roles":["time-source"],"endpoints":{"sim":"http://10.99.99.27:8080","sim":"http://10.99.99.27:8080"}}]`, `duplicate JSON key "sim" at $.services[0].endpoints`},
		{"two endpoints objects on one service", `[{"endpoints":{"sim":"http://10.99.99.27:8080"},"endpoints":{"sim":"http://10.99.99.27:8081"}}]`, `duplicate JSON key "endpoints" at $.services[0]`},
		{"escaped sim duplicate", `[{"endpoints":{"sim":"http://10.99.99.27:8080","\u0073im":"http://10.99.99.27:8081"}}]`, `duplicate JSON key "sim" at $.services[0].endpoints`},
		{"distinct endpoint names", `[{"endpoints":{"api":"http://10.99.99.27:8081","sim":"http://10.99.99.27:8080"}}]`, ""},
		{"duplicate other name", `[{"endpoints":{"api":"http://10.99.99.27:8080","api":"http://10.99.99.27:8081"}}]`, `duplicate JSON key "api" at $.services[0].endpoints`},
		{"null endpoints", `[{"endpoints":null}]`, ""},
		{"omitted endpoints", `[{}]`, ""},
		{"empty endpoints", `[{"endpoints":{}}]`, ""},
		{"array", `[{"endpoints":[]}]`, "cannot unmarshal array"},
		{"non-string URL", `[{"endpoints":{"sim":42}}]`, "cannot unmarshal number"},
		{"unknown service field", `[{"unknown":true,"endpoints":{"sim":"http://10.99.99.27:8080"}}]`, `unknown field "unknown"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			dir := Dir(root, "valid")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			body := `{"schema":2,"package":"valid","services":` + test.services + `}`
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			m, err := Load(root, "valid")
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Load error = %v, want %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Services) == 0 {
				t.Fatal("loaded no services")
			}
			if test.name == "many services" && (m.Services[0].Endpoints[EndpointSim] == "" || m.Services[1].Endpoints[EndpointSim] == "") {
				t.Fatal("sim endpoints must be retained independently per service")
			}
		})
	}
}
