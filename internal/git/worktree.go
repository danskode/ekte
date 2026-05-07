package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Worktree struct {
	Name   string
	Branch string
	Path   string
	Spec   string
}

// RepoRoot finder git-roden fra den givne mappe.
func RepoRoot(from string) (string, error) {
	out, err := run(from, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("ikke i et git-repo — kør 'git init' først")
	}
	return strings.TrimSpace(out), nil
}

// Create opretter spec-fil, branch og worktree.
func Create(repoRoot, name string) (*Worktree, error) {
	name = sanitize(name)
	if name == "" {
		return nil, fmt.Errorf("ugyldigt navn")
	}

	branch := "feature/" + name
	wtPath := filepath.Join(repoRoot, ".ekte", "worktrees", name)
	specPath := filepath.Join(repoRoot, "specs", name+".md")

	// opret spec-fil hvis den ikke findes
	if err := ensureSpec(specPath, name); err != nil {
		return nil, fmt.Errorf("spec: %w", err)
	}

	// opret worktree-mappe
	if err := os.MkdirAll(filepath.Dir(wtPath), 0755); err != nil {
		return nil, err
	}

	// tjek om branch allerede eksisterer
	_, branchErr := run(repoRoot, "git", "rev-parse", "--verify", branch)
	if branchErr != nil {
		// ny branch
		if _, err := run(repoRoot, "git", "worktree", "add", "-b", branch, wtPath); err != nil {
			return nil, fmt.Errorf("worktree add: %w", err)
		}
	} else {
		// branch eksisterer, checkout den
		if _, err := run(repoRoot, "git", "worktree", "add", wtPath, branch); err != nil {
			return nil, fmt.Errorf("worktree add (eksisterende branch): %w", err)
		}
	}

	return &Worktree{
		Name:   name,
		Branch: branch,
		Path:   wtPath,
		Spec:   specPath,
	}, nil
}

// List returnerer alle aktive worktrees oprettet af ekte.
func List(repoRoot string) ([]Worktree, error) {
	out, err := run(repoRoot, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("worktree list: %w", err)
	}

	var result []Worktree
	wtBase := filepath.Join(repoRoot, ".ekte", "worktrees")

	var curPath, curBranch string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "worktree ") {
			curPath = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			curBranch = strings.TrimPrefix(line, "branch ")
			curBranch = strings.TrimPrefix(curBranch, "refs/heads/")
		} else if line == "" && curPath != "" {
			// kun ekte-worktrees (under .ekte/worktrees/)
			if strings.HasPrefix(curPath, wtBase) {
				name := filepath.Base(curPath)
				result = append(result, Worktree{
					Name:   name,
					Branch: curBranch,
					Path:   curPath,
					Spec:   filepath.Join(repoRoot, "specs", name+".md"),
				})
			}
			curPath = ""
			curBranch = ""
		}
	}
	return result, nil
}

// Merge kører hooks og merger branch til main.
func Merge(repoRoot, name string, hookPaths []string) error {
	branch := "feature/" + name

	// kør hooks som gates
	for _, h := range hookPaths {
		full := filepath.Join(repoRoot, h)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			continue
		}
		out, err := runScript(full)
		if err != nil {
			return fmt.Errorf("hook '%s' fejlede — merge afbrudt:\n%s", h, out)
		}
	}

	// skift til main og merger
	if _, err := run(repoRoot, "git", "checkout", "main"); err != nil {
		if _, err := run(repoRoot, "git", "checkout", "master"); err != nil {
			return fmt.Errorf("kunne ikke skifte til main/master")
		}
	}
	if _, err := run(repoRoot, "git", "merge", "--no-ff", branch, "-m", "merge: "+name); err != nil {
		return fmt.Errorf("merge fejlede: %w", err)
	}

	return Remove(repoRoot, name)
}

// Remove sletter worktree og branch.
func Remove(repoRoot, name string) error {
	wtPath := filepath.Join(repoRoot, ".ekte", "worktrees", name)
	branch := "feature/" + name

	if _, err := run(repoRoot, "git", "worktree", "remove", "--force", wtPath); err != nil {
		// fortsæt selv ved fejl — ryd op manuelt
		_ = os.RemoveAll(wtPath)
	}
	if _, err := run(repoRoot, "git", "worktree", "prune"); err != nil {
		return fmt.Errorf("worktree prune: %w", err)
	}
	if _, err := run(repoRoot, "git", "branch", "-d", branch); err != nil {
		// branch er allerede merget, prøv force
		if _, err := run(repoRoot, "git", "branch", "-D", branch); err != nil {
			return fmt.Errorf("slet branch: %w", err)
		}
	}
	return nil
}

func ensureSpec(path, name string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // allerede eksisterer
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	title := strings.ReplaceAll(name, "-", " ")
	title = strings.Title(title)
	content := fmt.Sprintf("# Spec: %s\n\n## Status: draft\n\n## Intent\n\n[Beskriv hvad denne feature skal gøre]\n\n## Acceptkriterier\n\n- [ ] \n", title)
	return os.WriteFile(path, []byte(content), 0644)
}

func sanitize(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	var out strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		}
	}
	return strings.Trim(out.String(), "-")
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func runScript(path string) (string, error) {
	cmd := exec.Command("/bin/sh", path)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
