package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadInstalledPackages(t *testing.T) {
	for _, body := range []string{
		"rangerdanger-installed-packages-v1\neu\nus\n",
		"", "rangerdanger-installed-packages-v1\n",
		installedPackagesSchema + "-invalid\neu\n",
		"rangerdanger-installed-packages-v1\nunknown\n",
		"rangerdanger-installed-packages-v1\nus\neu\n",
		"rangerdanger-installed-packages-v1\neu\neu\n",
		"rangerdanger-installed-packages-v1\neu", "rangerdanger-installed-packages-v1\r\neu\r\n",
		"rangerdanger-installed-packages-v1\neu\n\n",
		"rangerdanger-installed-packages-v1\n../us\n",
	} {
		t.Run(body, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "data", "installed-packages")
			if err := os.Mkdir(filepath.Dir(file), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
			ids, err := ReadInstalledPackages(root, []string{"eu", "us"})
			if body == "rangerdanger-installed-packages-v1\neu\nus\n" {
				if err != nil || !reflect.DeepEqual(ids, []string{"eu", "us"}) {
					t.Fatalf("read = %v, %v", ids, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), file) || !strings.Contains(err.Error(), "setup") {
				t.Fatalf("read invalid record = %v, %v", ids, err)
			}
		})
	}
	if _, err := ReadInstalledPackages(t.TempDir(), []string{"us"}); err == nil {
		t.Fatal("missing record accepted")
	}
}

func configInstall(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	defs := filepath.Join(root, "lab-definitions")
	for _, id := range []string{"eu-iec104-substation", DefaultPackage} {
		dir := filepath.Join(defs, "packages", id)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "package.yml"), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "installed-packages"),
		[]byte("rangerdanger-installed-packages-v1\neu-iec104-substation\nus-dnp3-substation\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return root, defs
}

func TestLoadRequiresInstallationReceiptAndDefault(t *testing.T) {
	for _, body := range []string{"missing", "invalid\n", "rangerdanger-installed-packages-v1\neu-iec104-substation\n"} {
		t.Run(body, func(t *testing.T) {
			isolateConfigEnv(t)
			setRange(t)
			file := filepath.Join(os.Getenv("RANGERDANGER_ROOT"), "data", "installed-packages")
			if body == "missing" {
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(file, []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), file) || !strings.Contains(err.Error(), "setup") {
				t.Fatalf("startup accepted unusable install: %v", err)
			}
		})
	}
}
