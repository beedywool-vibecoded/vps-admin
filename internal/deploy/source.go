package deploy

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	BuildAuto       = "auto"
	BuildCompose    = "compose"
	BuildDockerfile = "dockerfile"
	BuildNode       = "node"
	BuildStatic     = "static"

	maxSource = 1 << 30 // 1 GiB unpacked
	maxFiles  = 100_000
)

var composeFiles = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// Detection is what the panel shows before the first deploy.
type Detection struct {
	Build  string `json:"build"`
	Port   int    `json:"port"`   // container port to proxy to, 0 if unknown
	Reason string `json:"reason"` // human readable
}

type packageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

// Detect picks a build from the file names at the app root. pkg is package.json if any,
// dockerfile the Dockerfile content if any.
func Detect(files []string, pkg, dockerfile string) Detection {
	for _, f := range composeFiles {
		if slices.Contains(files, f) {
			return Detection{BuildCompose, 0, "Docker Compose (" + f + "): every service runs as described"}
		}
	}
	if slices.Contains(files, "Dockerfile") {
		port := exposedPort(dockerfile)
		r := "Dockerfile found"
		if port > 0 {
			r += fmt.Sprintf(", it exposes port %d", port)
		}
		return Detection{BuildDockerfile, port, r}
	}
	if slices.Contains(files, "package.json") {
		var p packageJSON
		json.Unmarshal([]byte(pkg), &p)
		if p.Scripts["start"] != "" {
			return Detection{BuildNode, 3000, "Node app: npm run build if present, then npm start with PORT=3000"}
		}
		if p.Scripts["build"] != "" {
			return Detection{BuildStatic, 80, "Static site built with npm run build, served by nginx"}
		}
	}
	if slices.Contains(files, "index.html") {
		return Detection{BuildStatic, 80, "Static site served by nginx"}
	}
	return Detection{"", 0, "Nothing to build found: add a Dockerfile, a compose file, a package.json or an index.html"}
}

var exposeRe = regexp.MustCompile(`(?im)^\s*EXPOSE\s+(\d+)`)

func exposedPort(dockerfile string) int {
	if m := exposeRe.FindStringSubmatch(dockerfile); m != nil {
		p, _ := strconv.Atoi(m[1])
		return p
	}
	return 0
}

// installCmd matches the lockfile so builds are reproducible.
func installCmd(files []string) string {
	switch {
	case slices.Contains(files, "pnpm-lock.yaml"):
		return "corepack enable && pnpm install --frozen-lockfile"
	case slices.Contains(files, "yarn.lock"):
		return "corepack enable && yarn install --frozen-lockfile"
	case slices.Contains(files, "package-lock.json"):
		return "npm ci"
	}
	return "npm install"
}

// Commands override the steps of the generated Dockerfile; empty means the default.
type Commands struct {
	Install, Build, Start string
}

// GeneratedDockerfile returns the Dockerfile used for node and static builds. Static
// builds look for the usual output folders of front-end tools.
func GeneratedDockerfile(build string, files []string, hasBuildScript bool, cmd Commands) string {
	install := installCmd(files)
	if cmd.Install != "" {
		install = cmd.Install
	}
	switch build {
	case BuildNode:
		buildStep := "npm run build --if-present"
		if cmd.Build != "" {
			buildStep = cmd.Build
		}
		start := `["npm", "start"]`
		if cmd.Start != "" {
			start = `["sh", "-c", ` + strconv.Quote(cmd.Start) + `]`
		}
		return `FROM node:22-alpine
WORKDIR /app
COPY . .
RUN ` + install + `
RUN ` + buildStep + `
ENV NODE_ENV=production PORT=3000
EXPOSE 3000
CMD ` + start + `
`
	case BuildStatic:
		buildStep := "npm run build"
		if cmd.Build != "" {
			buildStep, hasBuildScript = cmd.Build, true
		}
		if !hasBuildScript {
			return `FROM nginx:1.27-alpine
COPY . /usr/share/nginx/html
EXPOSE 80
`
		}
		return `FROM node:22-alpine AS build
WORKDIR /app
COPY . .
RUN ` + install + `
RUN ` + buildStep + `
RUN for d in dist build out public; do if [ -f "$d/index.html" ]; then cp -r "$d" /site; exit 0; fi; done; echo "no dist/, build/, out/ or public/ folder with an index.html after the build" >&2; exit 1
FROM nginx:1.27-alpine
COPY --from=build /site /usr/share/nginx/html
EXPOSE 80
`
	}
	return ""
}

// Extract unpacks a GitHub tarball into dst, dropping its top-level folder. Paths that
// escape dst, links and device files are refused or skipped.
func Extract(r io.Reader, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var total int64
	files := 0
	root, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		// GitHub archives wrap everything in "owner-repo-sha/"; pax headers have no slash.
		_, rel, ok := strings.Cut(h.Name, "/")
		if !ok || rel == "" {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry escapes the folder: %s", h.Name)
		}
		if files++; files > maxFiles {
			return errors.New("the repository has too many files")
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if total += h.Size; total > maxSource {
				return errors.New("the repository is larger than 1 GiB")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				mode = 0o755
			}
			os.Remove(target) // replace in place on redeploys (compose keeps its folder)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, err = io.CopyN(f, tr, h.Size)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Only links that stay inside the tree, as written in the repo.
			link := filepath.Join(filepath.Dir(target), h.Linkname)
			if filepath.IsAbs(h.Linkname) || (link != root && !strings.HasPrefix(link, root+string(os.PathSeparator))) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		}
	}
}

// listRoot returns the names at dir, directories with a trailing "/".
func listRoot(dir string) []string {
	entries, _ := os.ReadDir(dir)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() {
			n += "/"
		}
		out = append(out, n)
	}
	return out
}

// safeSubdir resolves the app root folder inside the source tree.
func safeSubdir(src, rel string) (string, error) {
	rel = strings.Trim(filepath.Clean("/"+rel), "/")
	dir := filepath.Join(src, rel)
	if dir != src && !strings.HasPrefix(dir, src+string(os.PathSeparator)) {
		return "", errors.New("invalid root folder")
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("root folder %q not found in the repository", rel)
	}
	return dir, nil
}
