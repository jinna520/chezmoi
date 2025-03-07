package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"

	"github.com/bradenhilton/mozillainstallhash"
	"howett.net/plist"

	"github.com/twpayne/chezmoi/v2/internal/chezmoi"
)

type ioregData struct {
	value map[string]interface{}
}

func (c *Config) includeTemplateFunc(filename string) string {
	var absPath chezmoi.AbsPath
	if path.IsAbs(filename) {
		var err error
		absPath, err = chezmoi.NewAbsPathFromExtPath(filename, c.homeDirAbsPath)
		if err != nil {
			panic(err)
		}
	} else {
		absPath = c.SourceDirAbsPath.Join(chezmoi.RelPath(filename))
	}
	contents, err := c.fileSystem.ReadFile(absPath.String())
	if err != nil {
		returnTemplateError(err)
		return ""
	}
	return string(contents)
}

func (c *Config) ioregTemplateFunc() map[string]interface{} {
	if runtime.GOOS != "darwin" {
		return nil
	}

	if c.ioregData.value != nil {
		return c.ioregData.value
	}

	cmd := exec.Command("ioreg", "-a", "-l")
	output, err := c.baseSystem.IdempotentCmdOutput(cmd)
	if err != nil {
		returnTemplateError(fmt.Errorf("ioreg: %w", err))
		return nil
	}

	var value map[string]interface{}
	if _, err := plist.Unmarshal(output, &value); err != nil {
		returnTemplateError(fmt.Errorf("ioreg: %w", err))
		return nil
	}
	c.ioregData.value = value
	return value
}

func (c *Config) joinPathTemplateFunc(elem ...string) string {
	return filepath.Join(elem...)
}

func (c *Config) lookPathTemplateFunc(file string) string {
	switch path, err := exec.LookPath(file); {
	case err == nil:
		return path
	case errors.Is(err, exec.ErrNotFound):
		return ""
	default:
		returnTemplateError(err)
		return ""
	}
}

func (c *Config) mozillaInstallHashTemplateFunc(path string) string {
	mozillaInstallHash, err := mozillainstallhash.MozillaInstallHash(path)
	if err != nil {
		returnTemplateError(err)
		return ""
	}
	return mozillaInstallHash
}

func (c *Config) outputTemplateFunc(name string, args ...string) string {
	output, err := c.baseSystem.IdempotentCmdOutput(exec.Command(name, args...))
	if err != nil {
		returnTemplateError(err)
		return ""
	}
	// FIXME we should be able to return output directly, but
	// github.com/Masterminds/sprig's trim function only accepts strings
	return string(output)
}

func (c *Config) statTemplateFunc(name string) interface{} {
	switch info, err := c.fileSystem.Stat(name); {
	case err == nil:
		return map[string]interface{}{
			"name":    info.Name(),
			"size":    info.Size(),
			"mode":    int(info.Mode()),
			"perm":    int(info.Mode().Perm()),
			"modTime": info.ModTime().Unix(),
			"isDir":   info.IsDir(),
		}
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		returnTemplateError(err)
		return nil
	}
}

func returnTemplateError(err error) {
	panic(err)
}
