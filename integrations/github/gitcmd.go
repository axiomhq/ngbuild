package github

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/watchly/ngbuild/core"
)

func (g *Github) cloneAndMerge(directory string, config *core.BuildConfig) error {

	baseBranch := config.BaseBranch
	if baseBranch == "" {
		baseBranch = "master"
	}

	if config.HeadRepo == "" || config.HeadHash == "" || config.BaseRepo == "" {
		return errors.New("Config is not filled out properly")
	}

	script := fmt.Sprintf(`git clone -q --branch %s %s "%s"; `, baseBranch, config.BaseRepo, directory)
	script += fmt.Sprintf(`cd %s ; `, directory)
	script += fmt.Sprintf(`git remote add head %s ; `, config.HeadRepo)
	script += fmt.Sprintf(`git fetch head ; `)
	script += fmt.Sprintf(`git merge --no-edit --commit %s ; `, config.HeadHash)

	cmd := exec.Command("/bin/sh", "-c", "-e", script)
	output, err := cmd.Output()
	if err != nil {
		logcritf("Error cloning repo: \nscript: %s\nstdout: %s", script, string(output))
		return err
	}

	return nil
}
