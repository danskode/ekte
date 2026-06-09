package container

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultMemory    = "512m"
	defaultCPUs      = "1"
	defaultTimeout   = 300 * time.Second
	defaultMaxOutput = 128 * 1024 // 128 KB
	defaultWorkdir   = "/work"
)

// DetectRuntime finder den tilgængelige container-runtime.
// preferred="" = autodetect (docker → podman). Returnerer brugervenlig fejl
// med installationsvejledning hvis hverken docker eller podman er i PATH.
func DetectRuntime(preferred string) (string, error) {
	candidates := []string{"docker", "podman"}
	if preferred != "" {
		candidates = []string{preferred}
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c); err == nil {
			return c, nil
		}
	}
	if preferred != "" {
		return "", fmt.Errorf("'%s' ikke fundet i PATH", preferred)
	}
	return "", fmt.Errorf("hverken docker eller podman fundet i PATH")
}

// Spec beskriver én container-kørsel.
type Spec struct {
	Runtime     string
	Image       string
	Cmd         string
	WorkdirHost string        // absolut sti til projektmappe på host
	WorkdirCtr  string        // mountpoint i container — default "/work"
	Network     bool          // false = --network none
	Ports       []string      // ["8080:8080"]
	Memory      string        // fx "512m" — altid sat
	CPUs        string        // fx "1" — altid sat
	Env         []string      // eksplicitte KEY=VALUE — arves ikke fra host
	Timeout     time.Duration // 0 = defaultTimeout
	MaxOutput   int           // 0 = defaultMaxOutput
}

// Result indeholder resultatet af en container-kørsel.
type Result struct {
	Output    string
	ExitCode  int
	TimedOut  bool
	Truncated bool
}

// imageNameRe validerer at container image-navne kun indeholder tilladte tegn.
// Forhindrer shell-injection via image-navn fra config.
var imageNameRe = regexp.MustCompile(`^[a-zA-Z0-9._/:\-]+$`)

// portSpecRe validerer port-specifikationer på formen "8080:8080".
var portSpecRe = regexp.MustCompile(`^\d{1,5}:\d{1,5}$`)

// ValidateImage returnerer fejl hvis image-navn indeholder ugyldige tegn.
func ValidateImage(image string) error {
	if image == "" {
		return fmt.Errorf("container image-navn må ikke være tomt")
	}
	if !imageNameRe.MatchString(image) {
		return fmt.Errorf("ugyldigt container image-navn: %q (kun a-z, A-Z, 0-9, ., _, /, :, - tilladt)", image)
	}
	return nil
}

// Run bygger og eksekverer docker/podman run og returnerer resultatet.
// Output afkortes ved MaxOutput; processen afbrydes ved Timeout og containeren
// dræbes eksplicit via docker/podman kill for at undgå efterladte processer.
func Run(ctx context.Context, spec Spec) (Result, error) {
	if err := ValidateImage(spec.Image); err != nil {
		return Result{}, err
	}
	applyDefaults(&spec)

	name := "ekte-hook-" + uuid.New().String()[:8]

	args := buildArgs(spec, name)

	var timeoutCtx context.Context
	var cancel context.CancelFunc
	if spec.Timeout > 0 {
		timeoutCtx, cancel = context.WithTimeout(ctx, spec.Timeout)
	} else {
		timeoutCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, spec.Runtime, args...)

	cap := &cappedWriter{max: spec.MaxOutput}
	cmd.Stdout = cap
	cmd.Stderr = cap

	runErr := cmd.Run()

	var res Result
	res.Output = cap.String()
	res.Truncated = cap.truncated
	res.TimedOut = timeoutCtx.Err() == context.DeadlineExceeded

	if res.TimedOut {
		// exec.CommandContext sender SIGKILL til docker-processen, men containeren
		// kan stadig køre. Dræb den eksplicit via runtime kill.
		killCtx, killCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer killCancel()
		_ = exec.CommandContext(killCtx, spec.Runtime, "kill", name).Run()
	}

	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}

	if res.TimedOut {
		return res, nil
	}
	return res, runErr
}

// buildArgs konstruerer argumentlisten til docker/podman run.
func buildArgs(spec Spec, name string) []string {
	args := []string{
		"run", "--rm",
		"--name", name,
	}

	if !spec.Network {
		args = append(args, "--network", "none")
	}

	// Kør altid med minimale privileges — reducer angrebsflade ved container-escape.
	args = append(args,
		"--user=nobody:nogroup",
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
	)

	args = append(args, "--memory", spec.Memory)
	args = append(args, "--cpus", spec.CPUs)

	for _, p := range spec.Ports {
		if !portSpecRe.MatchString(p) {
			continue
		}
		// Valider at porter er i gyldigt interval 1-65535.
		parts := strings.SplitN(p, ":", 2)
		valid := true
		for _, part := range parts {
			n := 0
			if _, err := fmt.Sscanf(part, "%d", &n); err != nil || n < 1 || n > 65535 {
				valid = false
				break
			}
		}
		if valid {
			args = append(args, "-p", p)
		}
	}

	args = append(args,
		"-v", spec.WorkdirHost+":"+spec.WorkdirCtr+":rw",
		"-w", spec.WorkdirCtr,
	)

	for _, e := range spec.Env {
		args = append(args, "-e", e)
	}

	args = append(args, spec.Image, "sh", "-c", spec.Cmd)
	return args
}

func applyDefaults(spec *Spec) {
	if spec.WorkdirCtr == "" {
		spec.WorkdirCtr = defaultWorkdir
	}
	if spec.Memory == "" {
		spec.Memory = defaultMemory
	}
	if spec.CPUs == "" {
		spec.CPUs = defaultCPUs
	}
	if spec.Timeout == 0 {
		spec.Timeout = defaultTimeout
	}
	if spec.MaxOutput == 0 {
		spec.MaxOutput = defaultMaxOutput
	}
}

// cappedWriter er en io.Writer der stopper ved max bytes og sætter truncated=true.
type cappedWriter struct {
	buf       bytes.Buffer
	written   int
	max       int
	truncated bool
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if w.truncated {
		return len(p), nil
	}
	remaining := w.max - w.written
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil
	}
	n := len(p)
	if n > remaining {
		p = p[:remaining]
		w.truncated = true
	}
	written, err := w.buf.Write(p)
	w.written += written
	return n, err
}

func (w *cappedWriter) String() string {
	return strings.TrimRight(w.buf.String(), "\n")
}

// interface-check
var _ io.Writer = (*cappedWriter)(nil)
