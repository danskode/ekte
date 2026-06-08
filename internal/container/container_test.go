package container

import (
	"testing"
	"time"
)

func TestBuildArgs_defaults(t *testing.T) {
	spec := Spec{
		Runtime:     "docker",
		Image:       "alpine:3",
		Cmd:         "echo hej",
		WorkdirHost: "/home/user/projekt",
		WorkdirCtr:  "/work",
		Memory:      "512m",
		CPUs:        "1",
	}
	args := buildArgs(spec, "ekte-hook-test")

	expect := []string{
		"run", "--rm",
		"--name", "ekte-hook-test",
		"--network", "none",
		"--memory", "512m",
		"--cpus", "1",
		"-v", "/home/user/projekt:/work:rw",
		"-w", "/work",
		"alpine:3",
		"sh", "-c", "echo hej",
	}
	if len(args) != len(expect) {
		t.Fatalf("forkert antal args: %d (forventet %d)\nfik:      %v\nforventet: %v", len(args), len(expect), args, expect)
	}
	for i, a := range args {
		if a != expect[i] {
			t.Errorf("arg[%d]: fik %q, forventet %q", i, a, expect[i])
		}
	}
}

func TestBuildArgs_network_og_ports(t *testing.T) {
	spec := Spec{
		Runtime:     "docker",
		Image:       "maven:3.9",
		Cmd:         "mvn spring-boot:run",
		WorkdirHost: "/proj",
		WorkdirCtr:  "/work",
		Memory:      "1g",
		CPUs:        "2",
		Network:     true,
		Ports:       []string{"8080:8080", "8443:8443"},
	}
	args := buildArgs(spec, "ekte-hook-abc")

	// --network none må ikke være med
	for i, a := range args {
		if a == "--network" {
			t.Errorf("forventede ingen --network flag, men fandt det ved index %d", i)
		}
	}
	// -p flags skal være med
	found8080, found8443 := false, false
	for i, a := range args {
		if a == "-p" && i+1 < len(args) {
			if args[i+1] == "8080:8080" {
				found8080 = true
			}
			if args[i+1] == "8443:8443" {
				found8443 = true
			}
		}
	}
	if !found8080 {
		t.Error("mangler -p 8080:8080")
	}
	if !found8443 {
		t.Error("mangler -p 8443:8443")
	}
}

func TestBuildArgs_env(t *testing.T) {
	spec := Spec{
		Runtime:     "docker",
		Image:       "node:20",
		Cmd:         "npm test",
		WorkdirHost: "/proj",
		WorkdirCtr:  "/work",
		Memory:      "512m",
		CPUs:        "1",
		Env:         []string{"NODE_ENV=test", "CI=true"},
	}
	args := buildArgs(spec, "ekte-hook-env")

	foundNode, foundCI := false, false
	for i, a := range args {
		if a == "-e" && i+1 < len(args) {
			if args[i+1] == "NODE_ENV=test" {
				foundNode = true
			}
			if args[i+1] == "CI=true" {
				foundCI = true
			}
		}
	}
	if !foundNode {
		t.Error("mangler -e NODE_ENV=test")
	}
	if !foundCI {
		t.Error("mangler -e CI=true")
	}
}

func TestApplyDefaults(t *testing.T) {
	spec := Spec{}
	applyDefaults(&spec)

	if spec.WorkdirCtr != defaultWorkdir {
		t.Errorf("WorkdirCtr: %q (forventet %q)", spec.WorkdirCtr, defaultWorkdir)
	}
	if spec.Memory != defaultMemory {
		t.Errorf("Memory: %q (forventet %q)", spec.Memory, defaultMemory)
	}
	if spec.CPUs != defaultCPUs {
		t.Errorf("CPUs: %q (forventet %q)", spec.CPUs, defaultCPUs)
	}
	if spec.Timeout != defaultTimeout {
		t.Errorf("Timeout: %v (forventet %v)", spec.Timeout, defaultTimeout)
	}
	if spec.MaxOutput != defaultMaxOutput {
		t.Errorf("MaxOutput: %d (forventet %d)", spec.MaxOutput, defaultMaxOutput)
	}
}

func TestApplyDefaults_bevarEksisterende(t *testing.T) {
	spec := Spec{
		Memory:  "2g",
		CPUs:    "4",
		Timeout: 60 * time.Second,
	}
	applyDefaults(&spec)

	if spec.Memory != "2g" {
		t.Errorf("Memory overskrevet: %q", spec.Memory)
	}
	if spec.CPUs != "4" {
		t.Errorf("CPUs overskrevet: %q", spec.CPUs)
	}
	if spec.Timeout != 60*time.Second {
		t.Errorf("Timeout overskrevet: %v", spec.Timeout)
	}
}

func TestDetectRuntime_ukendt(t *testing.T) {
	_, err := DetectRuntime("findesikke")
	if err == nil {
		t.Error("forventede fejl ved ukendt runtime, men fik nil")
	}
}

func TestCappedWriter_afkorter(t *testing.T) {
	w := &cappedWriter{max: 10}
	n, err := w.Write([]byte("helloworld!"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 11 {
		t.Errorf("Write returnerede %d, forventet 11", n)
	}
	if !w.truncated {
		t.Error("forventede truncated=true")
	}
	if w.buf.Len() != 10 {
		t.Errorf("buf.Len()=%d, forventet 10", w.buf.Len())
	}
}

func TestCappedWriter_ingenAfkortering(t *testing.T) {
	w := &cappedWriter{max: 100}
	_, _ = w.Write([]byte("kort tekst"))
	if w.truncated {
		t.Error("forventede truncated=false")
	}
}
