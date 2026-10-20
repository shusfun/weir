package control

import (
	"strings"
	"testing"
)

func TestValidateSetupAccountRejectsWeakInput(t *testing.T) {
	if err := ValidateSetupAccount("not-an-email", "long-enough-password"); err == nil {
		t.Fatal("invalid email accepted")
	}
	if err := ValidateSetupAccount("admin@example.com", "short"); err == nil {
		t.Fatal("short password accepted")
	}
}

func TestApplySetupStopsBeforeInstallWhenDatabaseIsDown(t *testing.T) {
	path := t.TempDir() + "/config.json"
	err := ApplySetup(path, SetupAnswers{
		Database: DatabaseConfig{Host: "127.0.0.1", Port: 1, User: "migate", Database: "gate", SSLMode: "disable"},
		Redis:    RedisConfig{Address: "127.0.0.1:1"},
		Email:    "admin@example.com",
		Password: "long-enough-password",
		Confirm:  true,
	})
	if err == nil || err.Error() != "database connection failed" {
		t.Fatalf("err = %v", err)
	}
}

func TestApplySetupRejectsInstalledConfiguration(t *testing.T) {
	path := t.TempDir() + "/config.json"
	cfg := Config{Installed: true, Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)}
	if err := saveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	err := ApplySetup(path, SetupAnswers{
		Database: DatabaseConfig{Host: "127.0.0.1", Port: 5432, User: "migate", Database: "gate", SSLMode: "disable"},
		Redis:    RedisConfig{Address: "127.0.0.1:6379"},
		Email:    "admin@example.com",
		Password: "long-enough-password",
		Confirm:  true,
	})
	if err == nil || err.Error() != "already installed" {
		t.Fatalf("err = %v", err)
	}
	loaded, loadErr := LoadConfig(path)
	if loadErr != nil || !loaded.Installed {
		t.Fatalf("installed flag changed: %+v %v", loaded, loadErr)
	}
}

func TestRunSetupCLIRejectsInstalledConfigurationBeforePrompts(t *testing.T) {
	path := t.TempDir() + "/config.json"
	if err := saveConfig(path, Config{Installed: true, Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)}); err != nil {
		t.Fatal(err)
	}
	err := RunSetupCLI(path, strings.NewReader(""), func(string) (string, error) {
		t.Fatal("password prompt should not run")
		return "", nil
	})
	if err == nil || err.Error() != "already installed" {
		t.Fatalf("err = %v", err)
	}
}
