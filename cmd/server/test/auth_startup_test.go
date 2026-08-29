package test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestAuthenticationConfigurationFailuresAreLoggedAtStartup(t *testing.T) {
	binaryPath := buildCLI(t)
	baseEnv := []string{
		"TZ=UTC",
		"CPA_BASE_URL=http://127.0.0.1:8317",
		"CPA_MANAGEMENT_KEY=test-management-key",
	}
	for _, testCase := range []struct {
		name    string
		env     []string
		message string
	}{
		{
			name:    "missing auth setting and password",
			message: "AUTH_ENABLED is not set, so authentication defaults to true; LOGIN_PASSWORD is required",
		},
		{
			name:    "public example password",
			env:     []string{"AUTH_ENABLED=true", "LOGIN_PASSWORD=replace-with-your-login-password"},
			message: "LOGIN_PASSWORD must not use the public example value",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			command := exec.Command(binaryPath)
			command.Dir = t.TempDir()
			command.Env = append(append([]string{}, baseEnv...), testCase.env...)
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatal("expected invalid authentication configuration to stop startup")
			}
			text := string(output)
			if !strings.Contains(text, "initialize app") || !strings.Contains(text, testCase.message) {
				t.Fatalf("expected startup log to explain the authentication configuration error, got %q", text)
			}
		})
	}
}
