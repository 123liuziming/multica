package handler

import "testing"

func TestParsePullRequestURL_GitHub(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"canonical", "https://github.com/cli/cli/pull/8888", "https://github.com/cli/cli/pull/8888"},
		{"trailing slash", "https://github.com/cli/cli/pull/8888/", "https://github.com/cli/cli/pull/8888"},
		{"with query", "https://github.com/cli/cli/pull/8888?ref=email", "https://github.com/cli/cli/pull/8888"},
		{"with fragment", "https://github.com/cli/cli/pull/8888#discussion", "https://github.com/cli/cli/pull/8888"},
		{"www host", "https://www.github.com/cli/cli/pull/8888", "https://github.com/cli/cli/pull/8888"},
		{"uppercase host", "https://GitHub.com/cli/cli/pull/8888", "https://github.com/cli/cli/pull/8888"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := parsePullRequestURL(c.raw)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if p.Source != prSourceGitHub {
				t.Errorf("source = %q, want github", p.Source)
			}
			if p.HTMLURL != c.want {
				t.Errorf("normalized = %q, want %q", p.HTMLURL, c.want)
			}
			if p.RepoOwner == nil || *p.RepoOwner != "cli" {
				t.Errorf("repo_owner = %v, want cli", p.RepoOwner)
			}
			if p.RepoName == nil || *p.RepoName != "cli" {
				t.Errorf("repo_name = %v, want cli", p.RepoName)
			}
			if p.Number == nil || *p.Number != 8888 {
				t.Errorf("number = %v, want 8888", p.Number)
			}
		})
	}
}

func TestParsePullRequestURL_Aone(t *testing.T) {
	raw := "https://code.alibaba-inc.com/peida.lpd/sls_reg/codereview/27300570?spm=21540d8c.7bb5c0ab.0.0.22b3c9caV94w8A"
	p, err := parsePullRequestURL(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Source != prSourceAone {
		t.Errorf("source = %q, want aone", p.Source)
	}
	want := "https://code.alibaba-inc.com/peida.lpd/sls_reg/codereview/27300570"
	if p.HTMLURL != want {
		t.Errorf("normalized = %q, want %q", p.HTMLURL, want)
	}
	if p.RepoOwner == nil || *p.RepoOwner != "peida.lpd" {
		t.Errorf("repo_owner = %v, want peida.lpd", p.RepoOwner)
	}
	if p.RepoName == nil || *p.RepoName != "sls_reg" {
		t.Errorf("repo_name = %v, want sls_reg", p.RepoName)
	}
	if p.Number == nil || *p.Number != 27300570 {
		t.Errorf("number = %v, want 27300570", p.Number)
	}
}

func TestParsePullRequestURL_Rejects(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"javascript:alert(1)",
		"data:text/html,<h1>x</h1>",
		"ftp://github.com/cli/cli/pull/1",
		"http://localhost:3000/foo",
		"http://127.0.0.1/foo",
		"http://10.0.0.1/foo",
		"https://github.com/cli/cli", // missing /pull/<n>
		"https://github.com/cli/cli/issues/1",
		"https://gitlab.com/foo/bar/-/merge_requests/1",
		"https://bitbucket.org/foo/bar/pull-requests/1",
		"https://code.alibaba-inc.com/owner/repo",
	}
	for _, raw := range bad {
		t.Run(raw, func(t *testing.T) {
			if _, err := parsePullRequestURL(raw); err == nil {
				t.Errorf("expected error for %q", raw)
			}
		})
	}
}
