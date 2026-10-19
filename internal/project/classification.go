package project

import (
	"os"
	"path/filepath"

	workspacelayout "denova/internal/workspace"
)

// DetectType classifies a newly added content directory using the same markers
// as automatic Book discovery. Existing Projects keep their stored type.
func DetectType(path string) (Type, error) {
	canonical, err := canonicalDirectory(path, true)
	if err != nil {
		return "", err
	}
	if isBookWorkspace(canonical) {
		return TypeBook, nil
	}
	return TypeGeneral, nil
}

func isBookWorkspace(path string) bool {
	for _, marker := range []string{"book.json", "ideas.md", "brainstorm.md", "chapters", "setting"} {
		if _, err := os.Stat(filepath.Join(path, marker)); err == nil {
			return true
		}
	}
	for _, name := range []string{workspacelayout.DataDirName, workspacelayout.LegacyDataDirName} {
		directory := filepath.Join(path, name)
		if info, err := os.Stat(directory); err == nil && info.IsDir() && !workspacelayout.IsUserDataRoot(directory) {
			return true
		}
	}
	return false
}
