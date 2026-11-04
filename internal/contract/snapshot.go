package contract

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type archiveSymlink struct {
	name   string
	target string
}

func materialize(ctx context.Context, repoRoot, commit, hardDeadlineMS string) (string, func(), error) {
	if err := deadlineError(ctx); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp("", "deploy-it-snapshot-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	cmd := newBoundedCommand(ctx, "git", "--no-replace-objects", "-C", repoRoot, "archive", "--format=tar", commit)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return "", nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Env = gitEnvironment("SHIP_IT_DEADLINE_UNIX_MS=" + hardDeadlineMS)
	if err := cmd.Start(); err != nil {
		cleanup()
		return "", nil, err
	}
	extractErr := extractArchiveContext(ctx, dir, stdout)
	if extractErr != nil {
		stopCommand(cmd)
		_ = cmd.Wait()
		cleanup()
		return "", nil, extractErr
	}
	if err := cmd.Wait(); err != nil {
		if deadlineError(ctx) != nil {
			cleanup()
			return "", nil, errDeploymentDeadline
		}
		cleanup()
		return "", nil, fmt.Errorf("materialize shipped commit: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	if err := deadlineError(ctx); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}

func extractArchive(root string, reader io.Reader) error {
	return extractArchiveContext(context.Background(), root, reader)
}

func extractArchiveContext(ctx context.Context, root string, reader io.Reader) error {
	tarReader := tar.NewReader(reader)
	var symlinks []archiveSymlink
	for {
		if err := deadlineError(ctx); err != nil {
			return err
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		path, relative, err := archivePath(root, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeXGlobalHeader:
			// Git may emit a PAX global metadata record (not a filesystem
			// object) on macOS. tar.Reader applies it to following entries.
			// Ignoring only this metadata type preserves the path/link checks
			// for every materialized object.
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode)&0o777)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, contextReader{ctx: ctx, reader: tarReader})
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			if _, _, err := archiveLinkTarget(relative, header.Linkname); err != nil {
				return err
			}
			symlinks = append(symlinks, archiveSymlink{name: path, target: header.Linkname})
		default:
			return fmt.Errorf("unsupported archive entry type for %s", header.Name)
		}
	}
	for _, link := range symlinks {
		if err := os.MkdirAll(filepath.Dir(link.name), 0o700); err != nil {
			return err
		}
		if err := os.Symlink(link.target, link.name); err != nil {
			return err
		}
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := deadlineError(r.ctx); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func archivePath(root, name string) (string, string, error) {
	relative := filepath.Clean(filepath.FromSlash(name))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("unsafe archive path %q", name)
	}
	return filepath.Join(root, relative), relative, nil
}

func archiveLinkTarget(relative, target string) (string, string, error) {
	converted := filepath.FromSlash(target)
	if filepath.IsAbs(converted) {
		return "", "", fmt.Errorf("unsafe absolute symlink target %q", target)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(relative), converted))
	if resolved == ".." || strings.HasPrefix(resolved, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("symlink target escapes snapshot: %q", target)
	}
	return resolved, converted, nil
}
