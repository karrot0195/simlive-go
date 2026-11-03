package utils

import (
	"regexp"
	"strings"
)

// Cat is the minimal interface for reading a remote file's contents, used to
// seam planner tests away from rclone.
type Cat interface {
	Cat(remotePath string) (string, error)
}

func GetRenditionsByMaster(m3u8Path string, reader Cat) []string {
	renditionsAllow := GetEnv("ACTIVE_RENDITIONS", "360,720,1080")
	targets := strings.Split(renditionsAllow, ",")

	m3u8Content, err := reader.Cat(m3u8Path)
	if err != nil {
		return make([]string, len(targets))
	}

	return GetRenditionsFromMasterText(m3u8Content, targets)
}

func GetRenditionsFromMasterText(text string, targets []string) []string {
	if targets == nil {
		renditionsAllow := GetEnv("ACTIVE_RENDITIONS", "360,720,1080")
		targets = strings.Split(renditionsAllow, ",")
	}
	return ExtractResolutions(text, targets)
}

func ExtractResolutions(text string, targetHeights []string) []string {
	results := make([]string, len(targetHeights))

	for i, height := range targetHeights {
		// Matches RESOLUTION=...x{height} and captures ONLY the folder name before the first '/'
		pattern := `RESOLUTION=\d+x` + height + `[^\n]*\n(?:#[^\n]*\n)*([^\s/#]+)`
		re := regexp.MustCompile(pattern)

		matches := re.FindStringSubmatch(text)
		if len(matches) > 1 {
			val := matches[1]
			results[i] = val
		}
	}

	return results
}
