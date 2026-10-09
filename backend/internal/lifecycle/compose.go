package lifecycle

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ComposeBinary is where Dockerfile.backend installs the standalone
// Compose binary. The image carries no docker CLI.
const ComposeBinary = "/usr/local/bin/docker-compose"

// upWaitTimeout bounds `up --wait`; Compose fails the up once a service is
// still not healthy after it.
const upWaitTimeout = 300 * time.Second

// Compose runs the Compose operations of the range lifecycle. File is the
// absolute path of a package's Compose file for the configured mode.
type Compose interface {
	// Config returns `config --format json`, the normalized model.
	Config(ctx context.Context, file string) ([]byte, error)
	// Images returns `config --images`, one reference per service.
	Images(ctx context.Context, file string) ([]string, error)
	// Up starts the range and waits for its healthchecks; it never builds
	// or pulls.
	Up(ctx context.Context, file string) error
	// Down removes the range project by its labels alone.
	Down(ctx context.Context) error
}

// ComposeCLI runs the Compose binary with explicit argv and no shell.
type ComposeCLI struct {
	Binary  string
	Project string // Compose project name of the range
	Root    string // installation root, always the project directory
	// Dir is the working directory of every run. It must hold no Compose
	// file: a label-only down discovers one from its working directory and
	// would then act on that model.
	Dir string
	// Env is the complete environment of every run.
	Env []string
}

// ComposeEnv is the minimal environment for range Compose runs: the Docker
// client variables the backend itself has, plus the containd JWT secret the
// backend signs with. Compose reads every other interpolation input from
// root/.env, and a variable set here overrides that file.
func ComposeEnv(containdSecret string) []string {
	env := []string{"CONTAIND_JWT_SECRET=" + containdSecret}
	for _, key := range []string{"PATH", "HOME", "DOCKER_HOST", "DOCKER_API_VERSION", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

// modelArgs addresses one Compose file with the root as project directory.
func (c *ComposeCLI) modelArgs(file string) []string {
	return []string{"-p", c.Project, "--project-directory", c.Root, "-f", file}
}

func (c *ComposeCLI) configArgs(file string) []string {
	return append(c.modelArgs(file), "config", "--format", "json")
}

func (c *ComposeCLI) imagesArgs(file string) []string {
	return append(c.modelArgs(file), "config", "--images")
}

func (c *ComposeCLI) upArgs(file string) []string {
	return append(c.modelArgs(file), "up", "-d", "--wait",
		"--wait-timeout", strconv.Itoa(int(upWaitTimeout/time.Second)),
		"--no-build", "--pull", "never")
}

// downArgs carries no -f and no --project-directory: with the root as
// project directory Compose would load root/docker-compose.yml, the
// platform model.
func (c *ComposeCLI) downArgs() []string {
	return []string{"-p", c.Project, "down", "--remove-orphans"}
}

// Config implements Compose.
func (c *ComposeCLI) Config(ctx context.Context, file string) ([]byte, error) {
	return c.run(ctx, c.configArgs(file))
}

// Images implements Compose.
func (c *ComposeCLI) Images(ctx context.Context, file string) ([]string, error) {
	out, err := c.run(ctx, c.imagesArgs(file))
	if err != nil {
		return nil, err
	}
	var images []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		image := strings.TrimSpace(line)
		if image == "" || seen[image] {
			continue
		}
		seen[image] = true
		images = append(images, image)
	}
	return images, nil
}

// Up implements Compose.
func (c *ComposeCLI) Up(ctx context.Context, file string) error {
	_, err := c.run(ctx, c.upArgs(file))
	return err
}

// Down implements Compose.
func (c *ComposeCLI) Down(ctx context.Context) error {
	_, err := c.run(ctx, c.downArgs())
	return err
}

// stderrTail bounds how much Compose stderr an error carries.
const stderrTail = 2048

// run executes Compose and returns its stdout. Cancelling ctx kills the
// whole process group.
func (c *ComposeCLI) run(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.Binary, args...)
	cmd.Dir = c.Dir
	cmd.Env = c.Env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	killProcessGroupOnCancel(cmd)
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > stderrTail {
			detail = "..." + detail[len(detail)-stderrTail:]
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = fmt.Errorf("%w (%v)", ctxErr, err)
		}
		return nil, fmt.Errorf("docker-compose %s: %w: %s", strings.Join(args, " "), err, detail)
	}
	return stdout.Bytes(), nil
}
