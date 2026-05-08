package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/version"
)

const (
	defaultGitHubAPIBaseURL = "https://api.github.com"
	defaultGitHubOwner      = "Willxup"
	defaultGitHubRepo       = "cpa-usage-keeper"
	defaultRequestTimeout   = 10 * time.Second
)

var stableVersionPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

type Result struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	CanCompare      bool   `json:"canCompare"`
	Message         string `json:"message"`
}

type Checker struct {
	currentVersion string
	baseURL        string
	owner          string
	repo           string
	client         *http.Client
}

type Option func(*Checker)

func IsStableVersion(value string) bool {
	return stableVersionPattern.MatchString(strings.TrimSpace(value))
}

func CompareStableVersions(left, right string) (int, bool) {
	leftParts, ok := parseStableVersion(left)
	if !ok {
		return 0, false
	}
	rightParts, ok := parseStableVersion(right)
	if !ok {
		return 0, false
	}

	for i := 0; i < len(leftParts); i++ {
		if leftParts[i] < rightParts[i] {
			return -1, true
		}
		if leftParts[i] > rightParts[i] {
			return 1, true
		}
	}
	return 0, true
}

func parseStableVersion(value string) ([3]int, bool) {
	matches := stableVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if matches == nil {
		return [3]int{}, false
	}

	var parts [3]int
	for i := 0; i < 3; i++ {
		parsed, err := strconv.Atoi(matches[i+1])
		if err != nil {
			return [3]int{}, false
		}
		parts[i] = parsed
	}
	return parts, true
}

func NewChecker(currentVersion string, opts ...Option) *Checker {
	checker := &Checker{
		currentVersion: strings.TrimSpace(currentVersion),
		baseURL:        defaultGitHubAPIBaseURL,
		owner:          defaultGitHubOwner,
		repo:           defaultGitHubRepo,
		client:         &http.Client{Timeout: defaultRequestTimeout},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(checker)
		}
	}
	if checker.client == nil {
		checker.client = &http.Client{Timeout: defaultRequestTimeout}
	}
	if checker.baseURL == "" {
		checker.baseURL = defaultGitHubAPIBaseURL
	}
	if checker.owner == "" {
		checker.owner = defaultGitHubOwner
	}
	if checker.repo == "" {
		checker.repo = defaultGitHubRepo
	}
	return checker
}

func WithBaseURL(baseURL string) Option {
	return func(c *Checker) {
		c.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Checker) {
		c.client = client
	}
}

func WithRepository(owner, repo string) Option {
	return func(c *Checker) {
		c.owner = strings.TrimSpace(owner)
		c.repo = strings.TrimSpace(repo)
	}
}

func (c *Checker) Check(ctx context.Context) (Result, error) {
	if !IsStableVersion(c.currentVersion) {
		return Result{
			CurrentVersion:  c.currentVersion,
			UpdateAvailable: false,
			CanCompare:      false,
			Message:         "current build is not comparable",
		}, nil
	}

	latestVersion, ok, err := c.latestStableVersion(ctx)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{
			CurrentVersion:  c.currentVersion,
			UpdateAvailable: false,
			CanCompare:      false,
			Message:         "no comparable release found",
		}, nil
	}

	comparison, ok := CompareStableVersions(c.currentVersion, latestVersion)
	if !ok {
		return Result{
			CurrentVersion:  c.currentVersion,
			LatestVersion:   latestVersion,
			UpdateAvailable: false,
			CanCompare:      false,
			Message:         "current build is not comparable",
		}, nil
	}

	result := Result{
		CurrentVersion:  c.currentVersion,
		LatestVersion:   latestVersion,
		UpdateAvailable: comparison < 0,
		CanCompare:      true,
	}
	if result.UpdateAvailable {
		result.Message = fmt.Sprintf("new version available: %s", latestVersion)
		return result, nil
	}
	result.Message = "already on the latest version"
	return result, nil
}

func (c *Checker) latestStableVersion(ctx context.Context) (string, bool, error) {
	latestRelease, err := c.fetchLatestRelease(ctx)
	if err == nil && IsStableVersion(latestRelease) {
		return latestRelease, true, nil
	}

	tags, err := c.fetchTags(ctx)
	if err != nil {
		if latestRelease == "" {
			return "", false, err
		}
		return "", false, err
	}

	latestTag := ""
	for _, tag := range tags {
		if !IsStableVersion(tag) {
			continue
		}
		if latestTag == "" {
			latestTag = tag
			continue
		}
		comparison, ok := CompareStableVersions(latestTag, tag)
		if ok && comparison < 0 {
			latestTag = tag
		}
	}
	if latestTag == "" {
		return "", false, nil
	}
	return latestTag, true, nil
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

type githubTag struct {
	Name string `json:"name"`
}

func (c *Checker) fetchLatestRelease(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.baseURL, c.owner, c.repo)
	var payload githubRelease
	status, err := c.getJSON(ctx, url, &payload)
	if err != nil {
		if status == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(payload.TagName), nil
}

func (c *Checker) fetchTags(ctx context.Context) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/tags?per_page=100", c.baseURL, c.owner, c.repo)
	var payload []githubTag
	_, err := c.getJSON(ctx, url, &payload)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(payload))
	for _, tag := range payload {
		name := strings.TrimSpace(tag.Name)
		if name != "" {
			result = append(result, name)
		}
	}
	return result, nil
}

func (c *Checker) getJSON(ctx context.Context, url string, target any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func DefaultChecker() *Checker {
	return NewChecker(version.Version)
}
