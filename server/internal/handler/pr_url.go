package handler

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type prSource string

const (
	prSourceGitHub prSource = "github"
	prSourceAone   prSource = "aone"
)

type parsedPullRequestURL struct {
	Source       prSource
	HTMLURL      string  // normalized
	RepoOwner    *string // nil for URLs we recognized only by host
	RepoName     *string
	Number       *int32
	DerivedTitle string // hostname + path-tail; the link handler falls back to this when no enrichment is available
}

var aoneCodeReviewPathRe = regexp.MustCompile(`^/([^/]+)/([^/]+)/codereview/(\d+)$`)

// parsePullRequestURL normalizes raw and classifies it as Aone (the only
// source supported for manual linking). GitHub URLs are rejected here even
// though webhook auto-linking still populates github_pull_request rows —
// the manual path has no way to obtain the installation_id required to
// insert into that table, so we'd hit a NOT NULL constraint at INSERT.
//
// Normalization rules (so dedup on (workspace_id, html_url) is stable):
//   - http/https only (rejects javascript:, data:, ftp: — otherwise the
//     frontend's <a href={pr.html_url}> would XSS).
//   - localhost / loopback / private / link-local hosts are rejected (no
//     useful review URL lives there; also closes off SSRF-shaped abuse).
//   - host lowercased, default port stripped, query and fragment dropped,
//     trailing slash trimmed.
func parsePullRequestURL(raw string) (parsedPullRequestURL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parsedPullRequestURL{}, errors.New("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return parsedPullRequestURL{}, fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return parsedPullRequestURL{}, errors.New("url must be http or https")
	}
	if u.Host == "" {
		return parsedPullRequestURL{}, errors.New("url is missing a host")
	}
	host := strings.ToLower(u.Hostname())
	if isLocalHost(host) {
		return parsedPullRequestURL{}, errors.New("url host is not allowed")
	}

	port := u.Port()
	hostPort := host
	if port != "" && !isDefaultPort(scheme, port) {
		hostPort = host + ":" + port
	}
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		path = "/"
	}
	normalized := scheme + "://" + hostPort + path

	switch host {
	case "github.com", "www.github.com":
		// GitHub PRs reach the system through the webhook auto-linker, which
		// supplies the installation_id required by github_pull_request. The
		// manual-link UI has no way to obtain that, so we refuse here.
		return parsedPullRequestURL{}, errors.New("github pull requests are linked automatically via the GitHub App; manual linking is not supported")
	case "code.alibaba-inc.com":
		if m := aoneCodeReviewPathRe.FindStringSubmatch(path); m != nil {
			owner, repo := m[1], m[2]
			number, _ := strconv.Atoi(m[3])
			n := int32(number)
			return parsedPullRequestURL{
				Source:       prSourceAone,
				HTMLURL:      normalized,
				RepoOwner:    &owner,
				RepoName:     &repo,
				Number:       &n,
				DerivedTitle: fmt.Sprintf("%s/%s!%d", owner, repo, number),
			}, nil
		}
		return parsedPullRequestURL{}, errors.New("aone url must look like https://code.alibaba-inc.com/<owner>/<repo>/codereview/<number>")
	}
	return parsedPullRequestURL{}, errors.New("only github.com and code.alibaba-inc.com urls are supported")
}

func isDefaultPort(scheme, port string) bool {
	return (scheme == "http" && port == "80") || (scheme == "https" && port == "443")
}

// isLocalHost rejects loopback, link-local, and private IPv4 / IPv6 ranges.
// Storing review URLs that point at a developer machine has no use and
// invites SSRF-shaped abuse later.
func isLocalHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}
