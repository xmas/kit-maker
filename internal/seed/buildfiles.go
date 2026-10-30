package seed

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
)

// A spec example's Dockerfile COPYs files from its folder (scripts/,
// content/). A blueprint has no folder, so the Dockerfile block carries them
// as build files (payload "files", path → content); publishing and the
// download put them beside the YAML again, and the Dockerfile stays as the
// example wrote it.

var copyLine = regexp.MustCompile(`^\s*(?:COPY|ADD)\s+(.+?)\s*$`)

// buildFiles returns the files under dir (in fsys) that dockerfile's COPY
// and ADD lines read, keyed by their path in the folder. COPY --from,
// heredocs and URLs read nothing from it.
func buildFiles(fsys fs.FS, dir, dockerfile string) (map[string]any, error) {
	files := map[string]any{}
	for _, line := range strings.Split(dockerfile, "\n") {
		m := copyLine.FindStringSubmatch(line)
		if m == nil || strings.Contains(m[1], "<<") || strings.Contains(m[1], "--from") || strings.HasPrefix(m[1], "[") {
			continue
		}
		var paths []string
		for _, a := range strings.Fields(m[1]) {
			if !strings.HasPrefix(a, "--") {
				paths = append(paths, a)
			}
		}
		if len(paths) < 2 {
			continue
		}
		for _, src := range paths[:len(paths)-1] {
			if strings.Contains(src, "://") {
				continue
			}
			rel := path.Clean(src)
			p := path.Join(dir, rel)
			err := fs.WalkDir(fsys, p, func(f string, de fs.DirEntry, err error) error {
				if err != nil || de.IsDir() {
					return err
				}
				b, err := fs.ReadFile(fsys, f)
				if err != nil {
					return err
				}
				files[strings.TrimPrefix(f, dir+"/")] = string(b)
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("%s copies %s, which isn't in the example: %w", dir, src, err)
			}
		}
	}
	return files, nil
}
