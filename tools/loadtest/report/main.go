package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type runManifest struct {
	SchemaVersion  int               `json:"schema_version"`
	Scenario       string            `json:"scenario"`
	StartedAt      time.Time         `json:"started_at"`
	FinishedAt     *time.Time        `json:"finished_at,omitempty"`
	ActualDuration string            `json:"actual_duration,omitempty"`
	ExitCode       *int              `json:"exit_code,omitempty"`
	GitRevision    string            `json:"git_revision"`
	GitBranch      string            `json:"git_branch"`
	GitDirty       bool              `json:"git_dirty"`
	OS             string            `json:"os"`
	Architecture   string            `json:"architecture"`
	Kernel         string            `json:"kernel,omitempty"`
	CPU            string            `json:"cpu,omitempty"`
	CPUs           int               `json:"cpus"`
	MemoryMiB      int64             `json:"memory_mib,omitempty"`
	GoVersion      string            `json:"go_version,omitempty"`
	NodeVersion    string            `json:"node_version,omitempty"`
	DockerVersion  string            `json:"docker_version,omitempty"`
	NetworkID      string            `json:"network_id,omitempty"`
	Containers     []containerInfo   `json:"containers,omitempty"`
	Parameters     map[string]string `json:"parameters,omitempty"`
}

type containerInfo struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	ImageID      string `json:"image_id"`
	ImageCreated string `json:"image_created,omitempty"`
	CreatedAt    string `json:"container_created_at,omitempty"`
	MemoryLimit  int64  `json:"memory_limit_bytes"`
	NanoCPUs     int64  `json:"nano_cpus"`
}

var safeParameterNames = []string{
	"USERS", "BROWSERS", "WRITERS", "SIMULATED_USERS", "BROWSER_CLIENTS",
	"DURATION", "TRIGGER", "TRIGGER_INTERVAL", "TRIGGER_DELAY", "MODE",
	"VIEW", "FILTER_GAME", "DATASET", "GAMES", "REQUESTS", "CHAT_MESSAGES",
	"SEED", "SESSION_OFFSET", "CONNECT_MODE", "DROP_EVERY", "DELAY",
	"CUT_AFTER", "OFFLINE_FOR", "SSE_ONLY", "RESET_SSE", "ALLOW_RECONNECT",
	"ALLOW_MISSING_EVENTS",
}

type stringList []string

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}
func (s *stringList) String() string { return strings.Join(*s, ",") }

func main() {
	mode := flag.String("mode", "start", "start, finish, or scan")
	scenario := flag.String("scenario", "", "short safe scenario name")
	runDir := flag.String("run-dir", "", "existing private run directory")
	exitCode := flag.Int("exit-code", 0, "scenario command exit code")
	sessionsPath := flag.String("sessions", "", "private session JSONL for secret scan")
	keyPath := flag.String("metrics-key-file", "", "private metrics key for secret scan")
	scanStdin := flag.Bool("stdin", false, "scan standard input")
	var paths stringList
	flag.Var(&paths, "path", "report file or directory to scan; repeatable")
	flag.Parse()

	var err error
	switch *mode {
	case "start":
		var path string
		path, err = startRun(*scenario)
		if err == nil {
			fmt.Println(path)
		}
	case "finish":
		err = finishRun(*runDir, *exitCode)
	case "scan":
		var matches int
		matches, err = scanSecrets(paths, *sessionsPath, *keyPath, *scanStdin, os.Stdin)
		if err == nil {
			fmt.Println(matches)
		}
	default:
		err = errors.New("-mode must be start, finish, or scan")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadtest report:", err)
		os.Exit(1)
	}
}

func startRun(scenario string) (string, error) {
	if !validScenario(scenario) {
		return "", errors.New("scenario must use lowercase letters, digits, or hyphens")
	}
	root, err := reportRoot()
	if err != nil {
		return "", err
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return "", err
	}
	manifest, err := collectManifest(scenario, time.Now().UTC())
	if err != nil {
		return "", err
	}
	stamp := manifest.StartedAt.Format("20060102T150405Z")
	for suffix := 0; ; suffix++ {
		name := stamp + "-" + scenario
		if suffix > 0 {
			name = fmt.Sprintf("%s-%d", name, suffix)
		}
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", errors.New("cannot create private run directory")
		}
		if err := writeManifest(filepath.Join(dir, "manifest.json"), manifest, os.O_EXCL); err != nil {
			_ = os.Remove(dir)
			return "", err
		}
		return dir, nil
	}
}

func finishRun(runDir string, exitCode int) error {
	root, err := reportRoot()
	if err != nil {
		return err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return errors.New("invalid report root")
	}
	absDir, err := filepath.Abs(runDir)
	if err != nil {
		return errors.New("invalid run directory")
	}
	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("run directory is outside the private report root")
	}
	dirInfo, err := os.Lstat(absDir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 || dirInfo.Mode().Perm()&0077 != 0 {
		return errors.New("run directory is not private")
	}
	manifestPath := filepath.Join(absDir, "manifest.json")
	info, err := os.Lstat(manifestPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("run manifest is not a private regular file")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return errors.New("cannot read run manifest")
	}
	var manifest runManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.SchemaVersion != 1 || manifest.Scenario == "" || manifest.FinishedAt != nil {
		return errors.New("invalid or already finished run manifest")
	}
	finished := time.Now().UTC()
	manifest.FinishedAt = &finished
	manifest.ActualDuration = finished.Sub(manifest.StartedAt).String()
	manifest.ExitCode = &exitCode
	temp := manifestPath + ".tmp"
	if err := writeManifest(temp, manifest, os.O_EXCL); err != nil {
		return err
	}
	if err := os.Rename(temp, manifestPath); err != nil {
		_ = os.Remove(temp)
		return errors.New("cannot finish run manifest")
	}
	return nil
}

func collectManifest(scenario string, started time.Time) (runManifest, error) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return runManifest{}, errors.New("cannot locate repository root")
	}
	repo := strings.TrimSpace(string(root))
	revision := gitOutput(repo, "rev-parse", "HEAD")
	branch := gitOutput(repo, "branch", "--show-current")
	dirty := gitOutput(repo, "status", "--porcelain") != ""
	manifest := runManifest{
		SchemaVersion: 1,
		Scenario:      scenario,
		StartedAt:     started,
		GitRevision:   revision,
		GitBranch:     branch,
		GitDirty:      dirty,
		OS:            runtime.GOOS,
		Architecture:  runtime.GOARCH,
		CPUs:          runtime.NumCPU(),
		GoVersion:     runtime.Version(),
		Kernel:        commandOutput("uname", "-sr"),
		CPU:           cpuName(),
		MemoryMiB:     memoryMiB(),
		NodeVersion:   commandOutput("node", "--version"),
		DockerVersion: commandOutput("docker", "version", "--format", "{{.Server.Version}}"),
		NetworkID:     commandOutput("docker", "network", "inspect", "--format", "{{.Id}}", "lansurfer-loadtest_default"),
		Parameters:    safeParameters(),
	}
	manifest.Containers = dockerContainers()
	return manifest, nil
}

func reportRoot() (string, error) {
	root := os.Getenv("LANSURFER_LOADTEST_REPORT_ROOT")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("cannot locate home directory")
		}
		root = filepath.Join(home, "lansurfer-loadtest-reports")
	}
	return filepath.Abs(root)
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return errors.New("cannot create private report root")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("report root must be a private directory")
	}
	return nil
}

func writeManifest(path string, manifest runManifest, flags int) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|flags, 0600)
	if err != nil {
		return errors.New("cannot create private run manifest")
	}
	if err := json.NewEncoder(file).Encode(manifest); err != nil {
		_ = file.Close()
		return errors.New("cannot encode run manifest")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return errors.New("cannot sync run manifest")
	}
	if err := file.Close(); err != nil {
		return errors.New("cannot close run manifest")
	}
	return nil
}

func validScenario(scenario string) bool {
	if len(scenario) == 0 || len(scenario) > 48 {
		return false
	}
	for i, c := range scenario {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || (i > 0 && c == '-') {
			continue
		}
		return false
	}
	return scenario[0] >= 'a' && scenario[0] <= 'z'
}

func safeParameters() map[string]string {
	values := make(map[string]string)
	for _, name := range safeParameterNames {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" || strings.ContainsAny(value, "\r\n") {
			continue
		}
		if name == "FILTER_GAME" && len(value) > 100 {
			continue
		}
		values[name] = value
	}
	return values
}

func gitOutput(repo string, args ...string) string {
	command := append([]string{"-C", repo}, args...)
	return commandOutput("git", command...)
}

func commandOutput(name string, args ...string) string {
	output, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func dockerContainers() []containerInfo {
	ids := strings.Fields(commandOutput("docker", "ps", "--filter", "label=com.docker.compose.project=lansurfer-loadtest", "--format", "{{.ID}}"))
	containers := make([]containerInfo, 0, len(ids))
	for _, id := range ids {
		text := commandOutput("docker", "inspect", "--format", "{{.Name}}|{{.Config.Image}}|{{.Image}}|{{.Created}}|{{.HostConfig.Memory}}|{{.HostConfig.NanoCpus}}", id)
		parts := strings.Split(text, "|")
		if len(parts) != 6 {
			continue
		}
		var item containerInfo
		item.Name, item.Image, item.ImageID, item.CreatedAt = strings.TrimPrefix(parts[0], "/"), parts[1], parts[2], parts[3]
		_, _ = fmt.Sscan(parts[4], &item.MemoryLimit)
		_, _ = fmt.Sscan(parts[5], &item.NanoCPUs)
		image := commandOutput("docker", "image", "inspect", "--format", "{{.Created}}", item.ImageID)
		item.ImageCreated = image
		containers = append(containers, item)
	}
	return containers
}

func cpuName() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name") {
			if _, value, ok := strings.Cut(line, ":"); ok {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func memoryMiB() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			var kib int64
			if _, err := fmt.Sscanf(line, "MemTotal: %d kB", &kib); err == nil {
				return kib / 1024
			}
		}
	}
	return 0
}
func scanSecrets(paths []string, sessionsPath, keyPath string, scanStdin bool, stdin io.Reader) (int, error) {
	if sessionsPath == "" || keyPath == "" || len(paths) == 0 && !scanStdin {
		return 0, errors.New("scan requires session and metrics-key files plus paths or stdin")
	}
	sessionInfo, err := os.Lstat(sessionsPath)
	if err != nil || !sessionInfo.Mode().IsRegular() || sessionInfo.Mode()&os.ModeSymlink != 0 || sessionInfo.Mode().Perm()&0077 != 0 {
		return 0, errors.New("session file must be private")
	}
	sessionFile, err := os.Open(sessionsPath)
	if err != nil {
		return 0, errors.New("cannot read private session file")
	}
	defer sessionFile.Close()
	secrets := make([][]byte, 0, 401)
	scanner := bufio.NewScanner(sessionFile)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		var record struct {
			Session string `json:"session_token"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.Session == "" {
			return 0, errors.New("invalid private session file")
		}
		secrets = append(secrets, []byte(record.Session))
	}
	if scanner.Err() != nil {
		return 0, errors.New("cannot read private session file")
	}
	keyInfo, err := os.Lstat(keyPath)
	if err != nil || !keyInfo.Mode().IsRegular() || keyInfo.Mode()&os.ModeSymlink != 0 || keyInfo.Mode().Perm()&0077 != 0 {
		return 0, errors.New("metrics key file must be private")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil || len(bytes.TrimSpace(key)) == 0 {
		return 0, errors.New("cannot read private metrics key")
	}
	secrets = append(secrets, bytes.TrimSpace(key))

	matches := 0
	check := func(data []byte) {
		for _, secret := range secrets {
			if len(secret) > 0 && bytes.Contains(data, secret) {
				matches++
			}
		}
	}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return 0, errors.New("cannot scan requested path")
		}
		if info.IsDir() {
			err = filepath.WalkDir(path, func(filePath string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return errors.New("cannot scan report directory")
				}
				if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !entry.Type().IsRegular() {
					return nil
				}
				content, readErr := os.ReadFile(filePath)
				if readErr != nil {
					return errors.New("cannot scan report file")
				}
				check(content)
				return nil
			})
			if err != nil {
				return 0, err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return 0, errors.New("scan path must be a regular file or directory")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return 0, errors.New("cannot scan report file")
		}
		check(content)
	}
	if scanStdin {
		content, err := io.ReadAll(io.LimitReader(stdin, (64<<20)+1))
		if err != nil || len(content) > 64<<20 {
			return 0, errors.New("scan input exceeds size limit")
		}
		check(content)
	}
	return matches, nil
}
