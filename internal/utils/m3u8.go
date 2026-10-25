package utils

import (
	"regexp"
	"simlive/internal/storage"
	"strings"
)

func GetRenditionsByMaster(m3u8Path string, rclone *storage.RcloneService) []string {
	renditionsAllow := GetEnv("ACTIVE_RENDITIONS", "360,720,1080")
	targets := strings.Split(renditionsAllow, ",")

	m3u8Content, err := rclone.Cat(m3u8Path)
	if err != nil {
		return make([]string, len(targets))
	}

	return ExtractResolutions(string(m3u8Content), targets)
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
