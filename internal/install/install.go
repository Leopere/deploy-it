package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Leopere/deploy-it/internal/skilldoc"
)

func Local(copyBinary bool, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if copyBinary {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			return err
		}
		dest := filepath.Join(home, ".local", "bin", "deploy-it")
		if exe != dest {
			if err := copyAtomic(exe, dest, 0o755); err != nil {
				return err
			}
		}
		fmt.Fprintln(out, "Installed", dest)
	}
	for _, base := range []string{filepath.Join(home, ".codex", "skills"), filepath.Join(home, ".claude", "skills")} {
		dir := filepath.Join(base, "deploy-it")
		if err := writeAtomic(filepath.Join(dir, "SKILL.md"), []byte(skilldoc.SkillMD), 0o644); err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(dir, "agents", "openai.yaml"), []byte(skilldoc.OpenAIYAML), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "Installed the deploy-it skill for Codex and Claude.")
	return nil
}

func copyAtomic(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".deploy-it-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, dest)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".deploy-it-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
