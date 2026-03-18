package domain

import "testing"

func TestResolveEnvironmentFromCwd_ExactMatch(t *testing.T) {
	envs := map[string]*EnvironmentEntry{
		"feat-auth": {
			Local: &LocalMeta{WorktreePath: "/Users/jake/worktrees/myproject/feat-auth"},
		},
	}

	name, err := ResolveEnvironmentFromCwd("/Users/jake/worktrees/myproject/feat-auth", envs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "feat-auth" {
		t.Errorf("expected 'feat-auth', got '%s'", name)
	}
}

func TestResolveEnvironmentFromCwd_NestedPath(t *testing.T) {
	envs := map[string]*EnvironmentEntry{
		"feat-auth": {
			Local: &LocalMeta{WorktreePath: "/Users/jake/worktrees/myproject/feat-auth"},
		},
	}

	name, err := ResolveEnvironmentFromCwd("/Users/jake/worktrees/myproject/feat-auth/apps/backend", envs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "feat-auth" {
		t.Errorf("expected 'feat-auth', got '%s'", name)
	}
}

func TestResolveEnvironmentFromCwd_NoMatch(t *testing.T) {
	envs := map[string]*EnvironmentEntry{
		"feat-auth": {
			Local: &LocalMeta{WorktreePath: "/Users/jake/worktrees/myproject/feat-auth"},
		},
	}

	_, err := ResolveEnvironmentFromCwd("/Users/jake/some/other/path", envs)
	if err == nil {
		t.Fatal("expected error for no match")
	}
}

func TestResolveEnvironmentFromCwd_SkipsRemote(t *testing.T) {
	envs := map[string]*EnvironmentEntry{
		"remote-env": {
			Remote: &RemoteMeta{VMId: "vm-123"},
		},
	}

	_, err := ResolveEnvironmentFromCwd("/Users/jake/any/path", envs)
	if err == nil {
		t.Fatal("expected error when only remote envs exist")
	}
}

func TestResolveEnvironmentFromCwd_PrefixNotSubdir(t *testing.T) {
	envs := map[string]*EnvironmentEntry{
		"feat": {
			Local: &LocalMeta{WorktreePath: "/Users/jake/worktrees/myproject/feat"},
		},
	}

	// "/feat-auth" starts with "/feat" but is not a subdirectory
	_, err := ResolveEnvironmentFromCwd("/Users/jake/worktrees/myproject/feat-auth", envs)
	if err == nil {
		t.Fatal("expected error: path is a prefix match but not a subdirectory")
	}
}
