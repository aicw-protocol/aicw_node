package releases

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var versionPartPattern = regexp.MustCompile(`^\d+$`)

type LatestRelease struct {
	TagName        string `json:"tagName"`
	LatestVersion  string `json:"latestVersion"`
	ReleasesURL    string `json:"releasesUrl"`
	PublishedAt    string `json:"publishedAt,omitempty"`
	CurrentVersion string `json:"currentVersion,omitempty"`
	UpdateAvailable bool  `json:"updateAvailable"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

// NormalizeVersion strips v prefix and -gui suffix from release tags.
func NormalizeVersion(input string) string {
	value := strings.TrimSpace(input)
	value = strings.TrimPrefix(strings.ToLower(value), "v")
	value = strings.TrimSuffix(value, "-gui")
	return value
}

// CompareVersions returns -1, 0, or 1 for semver-ish x.y.z strings.
func CompareVersions(a, b string) int {
	left := parseVersionParts(a)
	right := parseVersionParts(b)
	length := len(left)
	if len(right) > length {
		length = len(right)
	}

	for index := 0; index < length; index++ {
		leftPart := 0
		rightPart := 0
		if index < len(left) {
			leftPart = left[index]
		}
		if index < len(right) {
			rightPart = right[index]
		}
		switch {
		case leftPart < rightPart:
			return -1
		case leftPart > rightPart:
			return 1
		}
	}

	return 0
}

func parseVersionParts(input string) []int {
	normalized := NormalizeVersion(input)
	if normalized == "" {
		return nil
	}

	rawParts := strings.Split(normalized, ".")
	parts := make([]int, 0, len(rawParts))
	for _, raw := range rawParts {
		if !versionPartPattern.MatchString(raw) {
			parts = append(parts, 0)
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			parts = append(parts, 0)
			continue
		}
		parts = append(parts, value)
	}
	return parts
}

func IsVersionNewer(candidate, baseline string) bool {
	return CompareVersions(candidate, baseline) > 0
}

func FetchLatestFromNodeWeb(baseURL, currentVersion string, client *http.Client) (*LatestRelease, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	endpoint := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/api/releases/latest"
	if strings.TrimSpace(currentVersion) != "" {
		endpoint += "?current=" + url.QueryEscape(NormalizeVersion(currentVersion))
	}

	resp, err := client.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("release check %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out LatestRelease
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.TagName) == "" {
		return nil, fmt.Errorf("release check returned empty tag")
	}
	if strings.TrimSpace(out.LatestVersion) == "" {
		out.LatestVersion = NormalizeVersion(out.TagName)
	}
	if strings.TrimSpace(currentVersion) != "" {
		out.CurrentVersion = NormalizeVersion(currentVersion)
		if !out.UpdateAvailable {
			out.UpdateAvailable = IsVersionNewer(out.LatestVersion, out.CurrentVersion)
		}
	}
	return &out, nil
}
