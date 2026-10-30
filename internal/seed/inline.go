package seed

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// A blueprint is only its YAML: there is no build context beside it, so a
// Dockerfile that COPYs files from its folder (the spec examples' scripts/
// and content/) can't be built from a blueprint. inlineContext rewrites
// each such COPY as heredoc COPYs carrying the files' contents, which
// BuildKit's Dockerfile frontend (1.4 and later, which the kit frontend
// uses) builds with no context at all.

var copyLine = regexp.MustCompile(`^(\s*)(COPY|ADD)\s+(.+?)\s*$`)

const heredocTag = "KITMAKER_EOF"

// inlineContext returns dockerfile with every COPY/ADD of files under dir
// (in fsys) replaced by heredoc COPYs. COPY --from, heredocs and URLs are
// left alone.
func inlineContext(fsys fs.FS, dir, dockerfile string) (string, error) {
	lines := strings.Split(dockerfile, "\n")
	var out []string
	for _, line := range lines {
		m := copyLine.FindStringSubmatch(line)
		if m == nil || strings.Contains(m[3], "<<") || strings.Contains(m[3], "--from") || strings.HasPrefix(strings.TrimSpace(m[3]), "[") {
			out = append(out, line)
			continue
		}
		indent, args := m[1], strings.Fields(m[3])
		var flags, paths []string
		for _, a := range args {
			if strings.HasPrefix(a, "--") {
				flags = append(flags, a)
			} else {
				paths = append(paths, a)
			}
		}
		if len(paths) < 2 || strings.Contains(paths[0], "://") {
			out = append(out, line)
			continue
		}
		srcs, dest := paths[:len(paths)-1], paths[len(paths)-1]
		var files []struct{ name, dest string }
		for _, src := range srcs {
			p := path.Join(dir, path.Clean(src))
			info, err := fs.Stat(fsys, p)
			if err != nil {
				return "", fmt.Errorf("%s copies %s, which isn't in the example: %w", dir, src, err)
			}
			if !info.IsDir() {
				d := dest
				if strings.HasSuffix(dest, "/") || len(srcs) > 1 {
					d = path.Join(dest, path.Base(src))
				}
				files = append(files, struct{ name, dest string }{p, d})
				continue
			}
			var under []string
			err = fs.WalkDir(fsys, p, func(f string, de fs.DirEntry, err error) error {
				if err == nil && !de.IsDir() {
					under = append(under, f)
				}
				return err
			})
			if err != nil {
				return "", err
			}
			sort.Strings(under)
			for _, f := range under {
				files = append(files, struct{ name, dest string }{f, path.Join(dest, strings.TrimPrefix(f, p+"/"))})
			}
		}
		out = append(out, fmt.Sprintf("%s# Inlined by Kit Maker (a blueprint has no build context): %s", indent, strings.TrimSpace(line)))
		for _, f := range files {
			b, err := fs.ReadFile(fsys, f.name)
			if err != nil {
				return "", err
			}
			content := strings.TrimSuffix(string(b), "\n")
			tag := heredocTag
			for n := 2; strings.Contains(content, tag); n++ {
				tag = fmt.Sprintf("%s_%d", heredocTag, n)
			}
			fl := flags
			// Embedded files lose their mode; a script keeps being runnable.
			if strings.HasPrefix(content, "#!") && !hasFlag(flags, "--chmod") {
				fl = append(append([]string(nil), flags...), "--chmod=755")
			}
			head := indent + "COPY "
			if len(fl) > 0 {
				head += strings.Join(fl, " ") + " "
			}
			out = append(out, fmt.Sprintf("%s<<'%s' %s", head, tag, f.dest), content, tag)
		}
	}
	return strings.Join(out, "\n"), nil
}

func hasFlag(flags []string, name string) bool {
	for _, f := range flags {
		if f == name || strings.HasPrefix(f, name+"=") {
			return true
		}
	}
	return false
}
