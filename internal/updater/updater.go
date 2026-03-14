package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Repo              string
	AppName           string
	Variant           string
	OS                string
	Arch              string
	CurrentVersion    string
	CurrentExecutable string
	Timeout           time.Duration
	AllowPrerelease   bool
	AuthToken         string
	MaxRetryCount     int
	RetryDelay        time.Duration
	RequireChecksum   bool
	Logger            func(string, ...interface{})
	HTTPClient        *http.Client
}

type ReleasePayload struct {
	TagName    string             `json:"tag_name"`
	Prerelease bool               `json:"prerelease"`
	Draft      bool               `json:"draft"`
	Assets     []releaseAssetJSON `json:"assets"`
}

type releaseAssetJSON struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type releaseVersion struct {
	Major, Minor, Patch int
	Cycle, Seq          int
}

const hexHashLen = 64

// SelfUpdate checks GitHub latest release and applies update if newer.
func SelfUpdate(ctx context.Context, cfg Config) (string, bool, error) {
	cfg = normalizeConfig(cfg)
	logf(cfg.Logger, "auto-update: repo=%s os=%s arch=%s variant=%s", cfg.Repo, cfg.OS, cfg.Arch, cfg.Variant)
	release, err := fetchLatestRelease(ctx, cfg)
	if err != nil {
		return "", false, err
	}

	if !cfg.AllowPrerelease && release.Prerelease {
		return "", false, fmt.Errorf("latest release is prerelease: %s", release.TagName)
	}
	if !isNewerVersion(release.TagName, cfg.CurrentVersion) {
		logf(cfg.Logger, "auto-update: already at latest version %s", cfg.CurrentVersion)
		return "", false, nil
	}

	asset, err := selectAsset(release.Assets, cfg)
	if err != nil {
		return "", false, err
	}

	logf(cfg.Logger, "auto-update: selected asset=%s", asset.Name)
	downloadedPath, err := downloadAsset(ctx, cfg, asset)
	if err != nil {
		return "", false, err
	}
	defer os.Remove(downloadedPath)

	if err := verifyChecksum(ctx, cfg, release.Assets, asset, downloadedPath); err != nil {
		return "", false, err
	}

	if err := replaceExecutable(cfg.CurrentExecutable, downloadedPath); err != nil {
		return "", false, err
	}
	return release.TagName, true, nil
}

func normalizeConfig(cfg Config) Config {
	cfg.OS = strings.TrimSpace(cfg.OS)
	if cfg.OS == "" {
		cfg.OS = runtime.GOOS
	}
	cfg.Arch = strings.TrimSpace(cfg.Arch)
	if cfg.Arch == "" {
		cfg.Arch = runtime.GOARCH
	}
	cfg.Repo = strings.TrimSpace(cfg.Repo)
	if cfg.Repo == "" {
		cfg.Repo = "dh-kam/tmux-llm-yolo"
	}
	cfg.AppName = strings.TrimSpace(cfg.AppName)
	if cfg.AppName == "" {
		cfg.AppName = "tmux-llm-yolo"
	}
	cfg.Variant = strings.TrimSpace(cfg.Variant)
	if cfg.Variant == "" {
		cfg.Variant = "release"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}
	if cfg.MaxRetryCount < 0 {
		cfg.MaxRetryCount = 2
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 500 * time.Millisecond
	}
	if cfg.Logger == nil {
		cfg.Logger = func(string, ...interface{}) {}
	}
	cfg.CurrentVersion = strings.TrimSpace(cfg.CurrentVersion)
	if cfg.CurrentVersion == "" {
		cfg.CurrentVersion = "0.0.0"
	}
	cfg.Variant = normalizeVariant(cfg.Variant)
	return cfg
}

func fetchLatestRelease(ctx context.Context, cfg Config) (ReleasePayload, error) {
	requestURL := "https://api.github.com/repos/" + cfg.Repo + "/releases/latest"
	resp, err := doGetWithRetry(ctx, cfg, requestURL, "github release latest")
	if err != nil {
		return ReleasePayload{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return ReleasePayload{}, fmt.Errorf("github release api status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload ReleasePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ReleasePayload{}, err
	}
	if payload.TagName == "" {
		return ReleasePayload{}, fmt.Errorf("release payload missing tag_name")
	}
	if payload.Draft {
		return ReleasePayload{}, fmt.Errorf("latest release is draft")
	}
	return payload, nil
}

func selectAsset(assets []releaseAssetJSON, cfg Config) (releaseAssetJSON, error) {
	var exact, fallback releaseAssetJSON
	foundExact := false
	foundFallback := false
	for _, asset := range assets {
		if !assetTargetsCurrentPlatform(asset.Name, cfg) {
			continue
		}
		assetVariant := parseVariantFromAsset(asset.Name)
		switch {
		case assetVariant != "" && assetVariant == cfg.Variant && asset.Name != "":
			exact = asset
			foundExact = true
		case !foundFallback:
			fallback = asset
			foundFallback = true
		}
	}
	if foundExact {
		return exact, nil
	}
	if foundFallback {
		return fallback, nil
	}
	return releaseAssetJSON{}, fmt.Errorf("no matching asset for os=%s arch=%s variant=%s", cfg.OS, cfg.Arch, cfg.Variant)
}

func assetTargetsCurrentPlatform(assetName string, cfg Config) bool {
	if !strings.HasPrefix(assetName, cfg.AppName+"_") {
		return false
	}
	parts := strings.Split(assetName, "_")
	if len(parts) < 4 {
		return false
	}
	if parts[2] != cfg.OS || parts[3] != cfg.Arch {
		return false
	}
	return true
}

func parseVariantFromAsset(assetName string) string {
	parts := strings.Split(assetName, "_")
	if len(parts) < 5 {
		return ""
	}
	variantCandidate := parts[4]
	variantCandidate = strings.TrimSuffix(variantCandidate, ".tar.gz")
	variantCandidate = strings.TrimSuffix(variantCandidate, ".zip")
	return normalizeVariant(variantCandidate)
}

func downloadAsset(ctx context.Context, cfg Config, asset releaseAssetJSON) (string, error) {
	resp, err := doGetWithRetry(ctx, cfg, asset.BrowserDownloadURL, "download asset "+asset.Name)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", fmt.Errorf("asset download status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	name := strings.ToLower(asset.Name)
	if strings.HasSuffix(name, ".tar.gz") {
		return extractBinaryFromTarGz(resp.Body, cfg.AppName)
	}
	if strings.HasSuffix(name, ".zip") {
		return extractBinaryFromZip(resp.Body, cfg.AppName)
	}
	return writeTempFile(resp.Body, cfg.AppName)
}

func downloadChecksumAsset(ctx context.Context, cfg Config, asset releaseAssetJSON) (string, error) {
	resp, err := doGetWithRetry(ctx, cfg, asset.BrowserDownloadURL, "download checksum asset "+asset.Name)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", fmt.Errorf("checksum download status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func verifyChecksum(
	ctx context.Context,
	cfg Config,
	assets []releaseAssetJSON,
	asset releaseAssetJSON,
	nextBinaryPath string,
) error {
	checksumAsset, ok := findChecksumAsset(assets, asset.Name)
	if !ok {
		if cfg.RequireChecksum {
			return fmt.Errorf("checksum file not found for asset=%s", asset.Name)
		}
		logf(cfg.Logger, "auto-update: checksum asset missing; continuing without checksum check")
		return nil
	}

	checksumText, err := downloadChecksumAsset(ctx, cfg, checksumAsset)
	if err != nil {
		if cfg.RequireChecksum {
			return err
		}
		logf(cfg.Logger, "auto-update: checksum fetch failed, skipping verification: %v", err)
		return nil
	}
	expectedHash, ok := parseChecksum(checksumText)
	if !ok {
		if cfg.RequireChecksum {
			return fmt.Errorf("checksum format unsupported for asset=%s", checksumAsset.Name)
		}
		logf(cfg.Logger, "auto-update: checksum format unsupported; continuing without verification")
		return nil
	}

	actualHash, err := calculateSHA256(nextBinaryPath)
	if err != nil {
		return err
	}
	if strings.EqualFold(actualHash, expectedHash) {
		logf(cfg.Logger, "auto-update: checksum verified (sha256)")
		return nil
	}

	if cfg.RequireChecksum {
		return fmt.Errorf("checksum mismatch for asset=%s expected=%s actual=%s", checksumAsset.Name, expectedHash, actualHash)
	}
	logf(cfg.Logger, "auto-update: checksum mismatch but RequireChecksum=false, 진행 계속: expected=%s actual=%s", expectedHash, actualHash)
	return nil
}

func findChecksumAsset(assets []releaseAssetJSON, assetName string) (releaseAssetJSON, bool) {
	candidates := checksumAssetCandidates(assetName)
	candidateSet := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		candidateSet[strings.ToLower(strings.TrimSpace(c))] = struct{}{}
	}
	for _, candidate := range assets {
		name := strings.TrimSpace(candidate.Name)
		if _, found := candidateSet[strings.ToLower(name)]; found {
			return candidate, true
		}
	}
	return releaseAssetJSON{}, false
}

func checksumAssetCandidates(assetName string) []string {
	name := strings.ToLower(strings.TrimSpace(assetName))
	base := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, ".tar.gz"), ".zip"), ".tgz")

	candidates := []string{
		name + ".sha256",
		name + ".sha256sum",
		name + ".sha256.txt",
		name + ".sha256sum.txt",
		name + ".checksum",
		name + ".check",
		base + ".sha256",
		base + ".sha256sum",
		base + ".sha256.txt",
		base + ".sha256sum.txt",
		base + ".checksum",
		base + ".check",
	}
	return candidates
}

func parseChecksum(raw string) (string, bool) {
	for _, field := range strings.Fields(raw) {
		v := strings.TrimSpace(strings.ToLower(field))
		if len(v) != hexHashLen {
			continue
		}
		if _, err := hex.DecodeString(v); err != nil {
			continue
		}
		return v, true
	}
	return "", false
}

func calculateSHA256(filePath string) (string, error) {
	in, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer in.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, in); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func doGetWithRetry(ctx context.Context, cfg Config, requestURL string, purpose string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetryCount; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", cfg.AppName)
		if token := strings.TrimSpace(cfg.AuthToken); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := cfg.HTTPClient.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK || !isRetryableStatus(resp.StatusCode) {
				return resp, nil
			}
			if attempt < cfg.MaxRetryCount {
				lastErr = fmt.Errorf("%s status=%d", purpose, resp.StatusCode)
				_ = resp.Body.Close()
			} else {
				return resp, nil
			}
		} else {
			lastErr = err
		}
		if attempt >= cfg.MaxRetryCount {
			if lastErr == nil {
				lastErr = fmt.Errorf("request failed for %s", requestURL)
			}
			return nil, lastErr
		}

		logf(cfg.Logger, "auto-update: retrying request in %s (%s, attempt=%d/%d): %v", retryDelayForAttempt(cfg, attempt), purpose, attempt+1, cfg.MaxRetryCount, lastErr)
		if err := sleepWithContext(ctx, retryDelayForAttempt(cfg, attempt)); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || (status >= 500 && status < 600)
}

func retryDelayForAttempt(cfg Config, attempt int) time.Duration {
	delay := cfg.RetryDelay
	for i := 0; i < attempt; i++ {
		delay *= 2
	}
	if delay > 10*time.Second {
		delay = 10 * time.Second
	}
	return delay
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	t := time.NewTimer(duration)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func extractBinaryFromTarGz(reader io.Reader, appName string) (string, error) {
	gzr, err := gzip.NewReader(reader)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		if filepath.Base(hdr.Name) != appName {
			continue
		}
		return writeTempFile(tr, appName)
	}

	return "", fmt.Errorf("binary %s not found in tar archive", appName)
}

func extractBinaryFromZip(reader io.Reader, appName string) (string, error) {
	tmpZip, err := os.CreateTemp("", appName+"-*.zip")
	if err != nil {
		return "", err
	}
	tmpZipPath := tmpZip.Name()
	defer func() {
		_ = tmpZip.Close()
		_ = os.Remove(tmpZipPath)
	}()

	if _, err = io.Copy(tmpZip, reader); err != nil {
		return "", err
	}
	if err = tmpZip.Close(); err != nil {
		return "", err
	}

	zipReader, err := zip.OpenReader(tmpZipPath)
	if err != nil {
		return "", err
	}
	defer zipReader.Close()

	targetBinary := filepath.Base(appName)
	targetBinaryWithExt := targetBinary + ".exe"
	for _, entry := range zipReader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		baseName := filepath.Base(entry.Name)
		if baseName != targetBinary && baseName != targetBinaryWithExt {
			continue
		}
		entryReader, err := entry.Open()
		if err != nil {
			return "", err
		}
		tempPath, copyErr := writeTempFile(entryReader, appName)
		_ = entryReader.Close()
		if copyErr != nil {
			return "", copyErr
		}
		return tempPath, nil
	}

	return "", fmt.Errorf("binary %s not found in zip archive", appName)
}

func writeTempFile(reader io.Reader, appName string) (string, error) {
	tmpPath, err := os.CreateTemp("", appName+"-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath.Name())
		}
	}()
	if _, err = io.Copy(tmpPath, reader); err != nil {
		return "", err
	}
	if err = tmpPath.Close(); err != nil {
		return "", err
	}
	if err = os.Chmod(tmpPath.Name(), 0o755); err != nil {
		return "", err
	}
	return tmpPath.Name(), nil
}

func replaceExecutable(currentExecutable string, nextBinaryPath string) error {
	existingPath, err := os.Executable()
	if err != nil {
		return err
	}
	existingPath = filepath.Clean(existingPath)
	if currentExecutable = strings.TrimSpace(currentExecutable); currentExecutable != "" {
		existingPath = currentExecutable
	}
	existingPath, err = filepath.EvalSymlinks(existingPath)
	if err != nil {
		return err
	}

	targetDir := filepath.Dir(existingPath)
	targetBase := filepath.Base(existingPath)
	working, err := os.CreateTemp(targetDir, targetBase+".updated-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = working.Close()
		_ = os.Remove(working.Name())
	}()

	src, err := os.Open(nextBinaryPath)
	if err != nil {
		return err
	}
	defer src.Close()

	if _, err := io.Copy(working, src); err != nil {
		return err
	}
	if err := working.Chmod(0o755); err != nil {
		return err
	}
	if err := working.Close(); err != nil {
		return err
	}

	backupPath := existingPath + ".old"
	if err := os.Rename(existingPath, backupPath); err != nil {
		return err
	}
	if err := os.Rename(working.Name(), existingPath); err != nil {
		_ = os.Rename(backupPath, existingPath)
		return err
	}
	_ = os.Remove(working.Name())
	_ = os.Remove(backupPath)
	return nil
}

func isNewerVersion(candidate, current string) bool {
	cand, okCand := parseReleaseVersion(candidate)
	cur, okCur := parseReleaseVersion(current)
	if okCand && okCur {
		return compareReleaseVersion(cand, cur) > 0
	}
	return strings.Compare(normalizeVersionString(candidate), normalizeVersionString(current)) > 0
}

func normalizeVersionString(raw string) string {
	return strings.TrimPrefix(strings.TrimSpace(raw), "v")
}

func parseReleaseVersion(raw string) (releaseVersion, bool) {
	version := releaseVersion{}
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "v")
	rawParts := strings.SplitN(raw, "-", 2)
	numParts := strings.Split(rawParts[0], ".")
	parseOK := true
	for idx, part := range numParts {
		if idx > 2 {
			break
		}
		if strings.TrimSpace(part) == "" {
			parseOK = false
			break
		}
		parsed, err := strconv.Atoi(part)
		if err != nil {
			parseOK = false
			break
		}
		switch idx {
		case 0:
			version.Major = parsed
		case 1:
			version.Minor = parsed
		case 2:
			version.Patch = parsed
		}
	}

	if len(rawParts) < 2 {
		if parseOK {
			return version, true
		}
		return version, false
	}
	meta := strings.TrimSpace(rawParts[1])
	metaParts := strings.Split(meta, ".")
	if len(metaParts) > 0 {
		if parsed, err := strconv.Atoi(metaParts[0]); err == nil {
			version.Cycle = parsed
		} else {
			parseOK = false
		}
	}
	if len(metaParts) > 1 {
		if parsed, err := strconv.Atoi(metaParts[1]); err == nil {
			version.Seq = parsed
		} else {
			parseOK = false
		}
	}
	return version, parseOK
}

func compareReleaseVersion(left, right releaseVersion) int {
	if left.Major != right.Major {
		return left.Major - right.Major
	}
	if left.Minor != right.Minor {
		return left.Minor - right.Minor
	}
	if left.Patch != right.Patch {
		return left.Patch - right.Patch
	}
	if left.Cycle != right.Cycle {
		return left.Cycle - right.Cycle
	}
	return left.Seq - right.Seq
}

func normalizeVariant(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return "release"
	}
	return value
}

func RestartSelf() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	args := os.Args
	cmd := exec.Command(executable, args[1:]...)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

func logf(logger func(string, ...interface{}), format string, args ...interface{}) {
	if logger == nil {
		return
	}
	logger(format, args...)
}
