// Package update implements a lightweight in-app updater that checks the
// project's GitHub releases and (on Windows/Linux single-binary builds) can
// download and apply a newer release, then relaunch.
package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"govault/internal/version"

	"github.com/minio/selfupdate"
	"golang.org/x/mod/semver"
)

const releasesAPI = "https://api.github.com/repos/ShaeOJ/GoVault/releases/latest"

// Info describes the result of a check.
type Info struct {
	Available    bool   `json:"available"`    // a newer release exists
	Current      string `json:"current"`      // running version
	Latest       string `json:"latest"`       // latest release tag
	Notes        string `json:"notes"`        // release body (markdown)
	ReleaseURL   string `json:"releaseUrl"`   // human release page
	AssetURL     string `json:"assetUrl"`     // download URL for this platform's asset
	AssetName    string `json:"assetName"`    // asset filename
	SelfApplies  bool   `json:"selfApplies"`  // true if we can swap the binary in place (win/linux)
	Error        string `json:"error,omitempty"`
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}
type ghRelease struct {
	TagName string    `json:"tag_name"`
	HTMLURL string    `json:"html_url"`
	Body    string    `json:"body"`
	Assets  []ghAsset `json:"assets"`
}

// canonical turns a tag like "v1.1.0" / "1.1.0" / "v1-beta1" into a
// semver-comparable string, best-effort. Non-semver tags return "".
func canonical(tag string) string {
	t := strings.TrimSpace(tag)
	if t == "" {
		return ""
	}
	if !strings.HasPrefix(t, "v") {
		t = "v" + t
	}
	return semver.Canonical(t) // "" if not valid semver
}

// platformAsset picks the release asset for the current OS/arch and reports
// whether we can self-apply it (single-binary win/linux) vs. must open the page.
func platformAsset(assets []ghAsset) (a ghAsset, selfApplies bool) {
	var want string
	switch runtime.GOOS {
	case "windows":
		want = fmt.Sprintf("GoVault-windows-%s.exe", runtime.GOARCH)
		selfApplies = true
	case "linux":
		want = fmt.Sprintf("GoVault-linux-%s", runtime.GOARCH)
		selfApplies = true
	case "darwin":
		want = fmt.Sprintf("GoVault-macos-%s.zip", runtime.GOARCH)
		selfApplies = false // .app bundle in a zip — can't swap a single exe
	}
	for _, as := range assets {
		if as.Name == want {
			return as, selfApplies
		}
	}
	return ghAsset{}, false
}

func fetchLatest() (*ghRelease, error) {
	req, _ := http.NewRequest(http.MethodGet, releasesAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "GoVault-updater")
	c := &http.Client{Timeout: 12 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api: %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// Check queries GitHub for the latest release and compares it to the running
// version. Never returns an error to the caller for network issues — it puts
// the reason in Info.Error and Available=false, so the UI can stay quiet.
func Check() Info {
	cur := version.Version
	info := Info{Current: cur}
	rel, err := fetchLatest()
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Latest = rel.TagName
	info.Notes = rel.Body
	info.ReleaseURL = rel.HTMLURL
	asset, self := platformAsset(rel.Assets)
	info.AssetURL = asset.URL
	info.AssetName = asset.Name
	info.SelfApplies = self && asset.URL != ""

	// Decide if newer. "dev" (untagged local build) always offers the release.
	cc, cl := canonical(cur), canonical(rel.TagName)
	switch {
	case cur == "dev":
		info.Available = true
	case cc != "" && cl != "":
		info.Available = semver.Compare(cl, cc) > 0
	default:
		// non-semver tag somewhere — fall back to "differs"
		info.Available = strings.TrimPrefix(rel.TagName, "v") != strings.TrimPrefix(cur, "v")
	}
	return info
}

// Apply downloads the given asset URL and replaces the running executable,
// then returns nil. Call Relaunch() afterward. Only valid when Info.SelfApplies.
func Apply(assetURL string) error {
	if assetURL == "" {
		return fmt.Errorf("no downloadable asset for this platform")
	}
	req, _ := http.NewRequest(http.MethodGet, assetURL, nil)
	req.Header.Set("User-Agent", "GoVault-updater")
	c := &http.Client{Timeout: 5 * time.Minute}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: %s", resp.Status)
	}
	// selfupdate writes to a temp file next to the exe, then atomically swaps
	// (on Windows it moves the running exe aside first).
	if err := selfupdate.Apply(resp.Body, selfupdate.Options{}); err != nil {
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return fmt.Errorf("apply failed and rollback failed: %v (rollback: %v)", err, rerr)
		}
		return fmt.Errorf("apply update: %w", err)
	}
	return nil
}

// Relaunch starts the (now-updated) executable and returns; the caller should
// quit the current process immediately after.
func Relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Start()
}

// Reader is exported for tests / callers that already hold the bytes.
var _ = io.Discard
