package project_test

import (
	"testing"

	"github.com/devtopo-app/devtopo/internal/project"
)

func TestKeyGroupsWorktreesByRemote(t *testing.T) {
	a := project.Key(project.GitInfo{RemoteURL: "git@github.com:acme/webapp.git", Root: `C:\main`}, `C:\main`)
	b := project.Key(project.GitInfo{RemoteURL: "https://github.com/acme/webapp.git", Root: `C:\wt\at-1`}, `C:\wt\at-1`)
	if a != b || a != "github.com/acme/webapp" {
		t.Fatalf("worktrees should share key: %q vs %q", a, b)
	}
}

func TestKeyFallsBackToCwdWhenNotGit(t *testing.T) {
	if k := project.Key(project.GitInfo{}, `C:\tmp\scratch`); k != `C:\tmp\scratch` {
		t.Fatalf("non-git keys on cwd, got %q", k)
	}
}

func TestNameHandlesBothSeparators(t *testing.T) {
	if project.Name("github.com/acme/webapp") != "webapp" {
		t.Fatal("slash name")
	}
	if project.Name(`C:\tmp\scratch`) != "scratch" {
		t.Fatal("backslash name")
	}
}
