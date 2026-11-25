package gitcli

import (
	"fmt"
	"strings"
)

// Reverse the diff in place so selection IDs retain their original line
// positions, then use the same upstream selection rules as partial commits.
func DiscardPatch(patch string, selection map[int]bool) (string, error) {
	lines := strings.Split(strings.TrimSuffix(patch, "\n"), "\n")
	from, to := "", ""
	deleted := false
	for _, line := range lines {
		if strings.HasPrefix(line, "--- ") {
			from = strings.TrimPrefix(line, "--- ")
		}
		if strings.HasPrefix(line, "+++ ") {
			to = strings.TrimPrefix(line, "+++ ")
		}
	}
	path := to
	if path == "/dev/null" {
		path = from
		deleted = true
	}
	oldPath := strings.Replace(path, "b/", "a/", 1)
	newPath := strings.Replace(path, "a/", "b/", 1)
	inside := false
	for i, line := range lines {
		if match := hunkHeader.FindStringSubmatch(line); match != nil {
			oldCount, newCount := match[2], match[4]
			if oldCount == "" {
				oldCount = "1"
			}
			if newCount == "" {
				newCount = "1"
			}
			lines[i] = fmt.Sprintf("@@ -%s,%s +%s,%s @@", match[3], newCount, match[1], oldCount)
			inside = true
			continue
		}
		if !inside {
			switch {
			case strings.HasPrefix(line, "--- "):
				if deleted {
					lines[i] = "--- /dev/null"
				} else {
					lines[i] = "--- " + oldPath
				}
			case strings.HasPrefix(line, "+++ "):
				lines[i] = "+++ " + newPath
			default:
				lines[i] = ""
			}
			continue
		}
		if strings.HasPrefix(line, "+") {
			lines[i] = "-" + line[1:]
		} else if strings.HasPrefix(line, "-") {
			lines[i] = "+" + line[1:]
		}
	}
	return SelectedPatch(strings.Join(lines, "\n")+"\n", selection)
}
