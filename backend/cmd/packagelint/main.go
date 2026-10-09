package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
	"gopkg.in/yaml.v3"
)

type lintRun struct {
	root     string
	problems []string
	packages []packageModel
}

type packageModel struct {
	id       string
	manifest *manifest.Manifest
	topology labs.Package
	source   []byte
	release  []byte
	dir      string
}

func main() {
	rootFlag := flag.String("root", "", "installation root (defaults to RANGERDANGER_ROOT or the discovered repository root)")
	flag.Parse()
	root, err := installationRoot(*rootFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	run := &lintRun{root: root}
	run.check()
	for _, problem := range run.problems {
		fmt.Println(problem)
	}
	if len(run.problems) > 0 {
		os.Exit(1)
	}
	fmt.Printf("package lint: %d package(s) passed\n", len(run.packages))
}

// packages is set by check after discovery for the final success summary.
func (run *lintRun) check() {
	packages, err := discoverPackages(run.root)
	if err != nil {
		run.problem("packages", err)
		return
	}
	run.packages = packages

	platform := make(map[manifest.Mode][]byte, 2)
	for _, mode := range []manifest.Mode{manifest.ModeSource, manifest.ModeRelease} {
		path := filepath.Join(run.root, platformComposeFile(mode))
		if err := manifest.CheckRawPlatformCompose(path); err != nil {
			run.problem("platform "+string(mode), err)
		}
		normalized, err := run.compose(manifest.PlatformProject, path)
		if err != nil {
			run.problem("platform "+string(mode)+" config", err)
			continue
		}
		platform[mode] = normalized
	}
	if len(platform[manifest.ModeSource]) > 0 && len(platform[manifest.ModeRelease]) > 0 {
		if err := manifest.CheckCrossMode(platform[manifest.ModeSource], platform[manifest.ModeRelease]); err != nil {
			run.problem("platform cross-mode", err)
		}
	}

	for index := range packages {
		pkg := &packages[index]
		if err := manifest.Validate(pkg.manifest, run.root); err != nil {
			run.problem(pkg.id+" manifest", err)
		}
		if err := manifest.CheckTopology(pkg.manifest, pkg.topology); err != nil {
			run.problem(pkg.id+" topology", err)
		}
		if len(platform[manifest.ModeSource]) > 0 {
			if err := manifest.CheckPlatform(pkg.manifest, platform[manifest.ModeSource]); err != nil {
				run.problem(pkg.id+" platform source", err)
			}
		}
		if len(platform[manifest.ModeRelease]) > 0 {
			if err := manifest.CheckPlatform(pkg.manifest, platform[manifest.ModeRelease]); err != nil {
				run.problem(pkg.id+" platform release", err)
			}
		}
		for _, mode := range []manifest.Mode{manifest.ModeSource, manifest.ModeRelease} {
			path := filepath.Join(pkg.dir, manifest.ComposeFile(mode))
			rawOK := true
			if err := manifest.CheckRawCompose(path); err != nil {
				run.problem(pkg.id+" "+string(mode)+" raw", err)
				rawOK = false
			}
			normalized, err := run.compose(manifest.RangeProject, path)
			if err != nil {
				run.problem(pkg.id+" "+string(mode)+" config", err)
				continue
			}
			if rawOK {
				if err := manifest.CheckCompose(pkg.manifest, mode, run.root, normalized); err != nil {
					run.problem(pkg.id+" "+string(mode)+" compose", err)
				}
			}
			if mode == manifest.ModeSource {
				pkg.source = normalized
			} else {
				pkg.release = normalized
			}
		}
		if len(pkg.source) > 0 && len(pkg.release) > 0 {
			if err := manifest.CheckCrossMode(pkg.source, pkg.release); err != nil {
				run.problem(pkg.id+" cross-mode", err)
			}
		}
		if err := run.checkNginx(pkg); err != nil {
			run.problem(pkg.id+" nginx", err)
		}
	}

	for left := 0; left < len(packages); left++ {
		for right := left + 1; right < len(packages); right++ {
			if len(packages[left].source) == 0 || len(packages[right].source) == 0 {
				continue
			}
			if err := manifest.CheckCrossPackage(packages[left].id, packages[left].source, packages[right].id, packages[right].source); err != nil {
				run.problem(packages[left].id+"/"+packages[right].id+" cross-package", err)
			}
		}
	}
}

func (run *lintRun) problem(label string, err error) {
	run.problems = append(run.problems, label+": "+singleLine(err.Error()))
}

func (run *lintRun) compose(project, file string) ([]byte, error) {
	args := []string{
		"compose", "-p", project,
		"--project-directory", run.root,
		"-f", file,
		"config", "--format", "json",
	}
	command := exec.Command("docker", args...)
	command.Env = envWith(os.Environ(), "RANGERDANGER_ROOT", run.root)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return nil, fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, singleLine(detail))
	}
	return stdout.Bytes(), nil
}

func (run *lintRun) checkNginx(pkg *packageModel) error {
	temporary, err := os.MkdirTemp("", "rd2a-b-lint-")
	if err != nil {
		return fmt.Errorf("create temporary nginx context: %w", err)
	}
	defer os.RemoveAll(temporary)
	routesDir := filepath.Join(temporary, "rd-routes")
	if err := os.Mkdir(routesDir, 0o700); err != nil {
		return fmt.Errorf("create temporary routes directory: %w", err)
	}
	routes, err := os.ReadFile(filepath.Join(pkg.dir, manifest.RoutesFile))
	if err != nil {
		return fmt.Errorf("read %s: %w", manifest.RoutesFile, err)
	}
	if err := os.WriteFile(filepath.Join(routesDir, "range.conf"), routes, 0o600); err != nil {
		return fmt.Errorf("write temporary routes file: %w", err)
	}
	name := fmt.Sprintf("rd2a-b-lint-%d-%d", os.Getpid(), time.Now().UnixNano())
	args := []string{
		"run", "--rm", "--name", name,
		"--add-host", "backend:127.0.0.1",
		"--add-host", "frontend:127.0.0.1",
		"-v", filepath.Join(run.root, "proxy", "nginx.conf") + ":/etc/nginx/nginx.conf:ro",
		"-v", routesDir + ":/etc/nginx/rd-routes:ro",
		"nginx:1.27-alpine", "nginx", "-t",
	}
	command := exec.Command("docker", args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, singleLine(detail))
	}
	return nil
}

func discoverPackages(root string) ([]packageModel, error) {
	directories, err := filepath.Glob(filepath.Join(root, "lab-definitions", "packages", "*"))
	if err != nil {
		return nil, fmt.Errorf("find package directories: %w", err)
	}
	var result []packageModel
	for _, directory := range directories {
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() {
			continue
		}
		manifestPath := filepath.Join(directory, manifest.FileName)
		if _, err := os.Stat(manifestPath); err != nil {
			// A package directory without the deployment manifest is still an
			// input and must not silently disappear from the lint run.
			return nil, fmt.Errorf("package directory %s has no %s", directory, manifest.FileName)
		}
		id := filepath.Base(directory)
		loaded, err := manifest.Load(root, id)
		if err != nil {
			return nil, fmt.Errorf("package %s: %w", id, err)
		}
		topology, err := loadTopologyPackage(root, directory)
		if err != nil {
			return nil, fmt.Errorf("package %s: %w", id, err)
		}
		result = append(result, packageModel{
			id:       id,
			manifest: loaded,
			topology: topology,
			dir:      directory,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	if len(result) == 0 {
		return nil, fmt.Errorf("no packages found in %s", filepath.Join(root, "lab-definitions", "packages"))
	}
	return result, nil
}

func loadTopologyPackage(root, packageDir string) (labs.Package, error) {
	var packageYAML labs.PackageYAML
	if err := decodeYAMLFile(filepath.Join(packageDir, "package.yml"), &packageYAML); err != nil {
		return labs.Package{}, err
	}
	if packageYAML.ID != filepath.Base(packageDir) {
		return labs.Package{}, fmt.Errorf("package.yml id %q does not match directory %q", packageYAML.ID, filepath.Base(packageDir))
	}
	topologyPath := filepath.Clean(filepath.Join(packageDir, packageYAML.Topology))
	relative, err := filepath.Rel(root, topologyPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return labs.Package{}, fmt.Errorf("topology path %q escapes installation root", packageYAML.Topology)
	}
	var topology labs.LabYAML
	if err := decodeYAMLFile(topologyPath, &topology); err != nil {
		return labs.Package{}, fmt.Errorf("topology: %w", err)
	}
	return labs.Package{ID: packageYAML.ID, Template: topology}, nil
}

func decodeYAMLFile(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%s must contain one YAML document", path)
	}
	return nil
}

func installationRoot(argument string) (string, error) {
	root := argument
	if root == "" {
		root = os.Getenv("RANGERDANGER_ROOT")
	}
	if root != "" {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return "", fmt.Errorf("resolve installation root: %w", err)
		}
		return absolute, nil
	}
	current, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for directory := current; ; directory = filepath.Dir(directory) {
		if isInstallationRoot(directory) {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
	}
	return "", fmt.Errorf("installation root not found; pass -root or set RANGERDANGER_ROOT")
}

func isInstallationRoot(path string) bool {
	for _, name := range []string{"docker-compose.yml", "docker-compose.release.yml"} {
		if _, err := os.Stat(filepath.Join(path, name)); err != nil {
			return false
		}
	}
	info, err := os.Stat(filepath.Join(path, "lab-definitions", "packages"))
	return err == nil && info.IsDir()
}

func platformComposeFile(mode manifest.Mode) string {
	if mode == manifest.ModeSource {
		return "docker-compose.yml"
	}
	return "docker-compose.release.yml"
}

func envWith(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
